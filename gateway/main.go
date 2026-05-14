package main

import (
	"container/heap"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// Tipos de Mensagem
const (
	MsgRequest         = "REQUEST"
	MsgReply           = "REPLY"
	MsgRelease         = "RELEASE"
	MsgHeartbeat       = "HEARTBEAT"
	MsgDroneFailed     = "DRONE_FAILED"
	MsgSnapshotRequest = "SNAPSHOT_REQUEST"
	MsgStateSync       = "STATE_SYNC"
	MsgAlert           = "ALERT"
	MsgDeviceReg       = "DEVICE_REG"
	MsgStatusReq       = "STATUS_REQ"
)

// Status do Drone
const (
	DroneAvailable = "DISPONIVEL"
	DroneBusy      = "OCUPADO"
	DroneFailed    = "FALHO"
)

// Estrutura de Mensagem Universal
type Message struct {
	Type       string            `json:"type"`
	DroneID    string            `json:"drone_id,omitempty"`
	GatewayID  string            `json:"gateway_id,omitempty"`
	Priority   int               `json:"priority,omitempty"`
	Lamport    int               `json:"lamport"`
	Timestamp  int64             `json:"timestamp,omitempty"`
	Payload    map[string]string `json:"payload,omitempty"` // Usado para State Sync
	Content    string            `json:"content,omitempty"`
	Occurrence string            `json:"occurrence,omitempty"`
}

// Estrutura de Requisição para a Fila de Prioridade
type AlertRequest struct {
	Occurrence string
	Priority   int
	Lamport    int
	GatewayID  string
	Timestamp  int64
}

// Fila de Prioridade (Min-Heap)
type PriorityQueue []*AlertRequest

func (pq PriorityQueue) Len() int { return len(pq) }
func (pq PriorityQueue) Less(i, j int) bool {
	if pq[i].Priority != pq[j].Priority {
		return pq[i].Priority < pq[j].Priority // Menor número = maior prioridade
	}
	if pq[i].Lamport != pq[j].Lamport {
		return pq[i].Lamport < pq[j].Lamport // Menor relógio = chegou antes
	}
	return pq[i].GatewayID < pq[j].GatewayID // Desempate lexicográfico
}
func (pq PriorityQueue) Swap(i, j int)       { pq[i], pq[j] = pq[j], pq[i] }
func (pq *PriorityQueue) Push(x interface{}) { *pq = append(*pq, x.(*AlertRequest)) }
func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}

// Variáveis Globais e Estado do Gateway
var (
	gatewayID     string
	lamportClock  int
	clockMutex    sync.Mutex
	stateMutex    sync.Mutex
	drones        = make(map[string]string) // DroneID -> Status
	droneOwners   = make(map[string]string) // DroneID -> GatewayID (Dono atual do R-A)
	droneAddrs    = make(map[string]string) // DroneID -> IP:Port (Para despachar ordens)
	peers         []string
	deferred      = make(map[string][]Message) // DroneID -> Lista de requests adiados
	repliesCount  = make(map[string]int)       // DroneID -> Contagem de Replies recebidos
	requestingCS  = make(map[string]bool)      // DroneID -> Se este gateway está pedindo R-A
	myCurrentReq  = make(map[string]Message)   // DroneID -> Minha requisição R-A atual
	reqQueue      PriorityQueue                // Fila local de ocorrências não atendidas
	activeBeacons = make(map[string]time.Time) // Rastreio de beacons locais
)

func mustEnv(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		log.Fatalf("[FATAL] Variável de ambiente obrigatória ausente: %s", key)
	}
	return v
}

func tickLamport(recvClock int) int {
	clockMutex.Lock()
	defer clockMutex.Unlock()
	if recvClock > lamportClock {
		lamportClock = recvClock
	}
	lamportClock++
	return lamportClock
}

func main() {
	heap.Init(&reqQueue)

	gatewayID = mustEnv("GATEWAY_ID")
	gatewayIP := mustEnv("GATEWAY_IP")
	gatewayHost := mustEnv("GATEWAY_HOST")
	regPort := mustEnv("GATEWAY_TCP_REG_PORT")
	clientPort := mustEnv("GATEWAY_TCP_CLIENT_PORT")
	peerPort := mustEnv("GATEWAY_TCP_PEER_PORT")

	// Configurar Peers
	peers = []string{
		fmt.Sprintf("%s:%s", mustEnv("IP_NORTE"), peerPort),
		fmt.Sprintf("%s:%s", mustEnv("IP_SUL"), peerPort),
		fmt.Sprintf("%s:%s", mustEnv("IP_LESTE"), peerPort),
		fmt.Sprintf("%s:%s", mustEnv("IP_OESTE"), peerPort),
	}

	log.Printf("[GATEWAY/%s] Iniciando... IP: %s", gatewayID, gatewayIP)

	go startServer(gatewayHost, peerPort, handlePeerConnection)
	go startServer(gatewayHost, regPort, handleRegConnection)
	go startServer(gatewayHost, clientPort, handleClientConnection)

	go syncStateOnStart()
	go processQueueLoop()

	select {} // Mantém o processo rodando
}

func startServer(host, port string, handler func(net.Conn)) {
	addr := fmt.Sprintf("%s:%s", host, port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("[FATAL] Falha ao escutar na porta %s: %v", port, err)
	}
	log.Printf("[GATEWAY/%s] Escutando TCP em %s", gatewayID, addr)
	for {
		conn, err := l.Accept()
		if err != nil {
			continue
		}
		go handler(conn)
	}
}

// ---- COMUNICAÇÃO INTER-GATEWAY (PEERS) ----
func handlePeerConnection(conn net.Conn) {
	defer conn.Close()
	decoder := json.NewDecoder(conn)
	var msg Message
	if err := decoder.Decode(&msg); err == nil {
		tickLamport(msg.Lamport)
		switch msg.Type {
		case MsgRequest:
			handleRARequest(msg, conn)
		case MsgReply:
			handleRAReply(msg)
		case MsgRelease:
			handleRARelease(msg)
		case MsgSnapshotRequest:
			sendStateSync(conn)
		case MsgStateSync:
			receiveStateSync(msg)
		case MsgDroneFailed:
			handleDroneFailed(msg)
		}
	}
}

func broadcastPeerMsg(msg Message) {
	for _, peer := range peers {
		// Não manda para si mesmo se o IP local coincidir, ou tenta e ignora falha
		go func(p string) {
			conn, err := net.DialTimeout("tcp", p, 2*time.Second)
			if err != nil {
				return // Peer offline, ignorado
			}
			defer conn.Close()
			json.NewEncoder(conn).Encode(msg)
		}(peer)
	}
}

func syncStateOnStart() {
	time.Sleep(2 * time.Second) // Aguarda inicialização
	msg := Message{
		Type:      MsgSnapshotRequest,
		GatewayID: gatewayID,
		Lamport:   tickLamport(0),
	}
	broadcastPeerMsg(msg)
}

func sendStateSync(conn net.Conn) {
	stateMutex.Lock()
	defer stateMutex.Unlock()
	payload := make(map[string]string)
	for dID, status := range drones {
		payload["drone_status_"+dID] = status
	}
	for dID, addr := range droneAddrs {
		payload["drone_addr_"+dID] = addr
	}
	msg := Message{
		Type:      MsgStateSync,
		GatewayID: gatewayID,
		Lamport:   tickLamport(0),
		Payload:   payload,
	}
	json.NewEncoder(conn).Encode(msg)
}

func receiveStateSync(msg Message) {
	stateMutex.Lock()
	defer stateMutex.Unlock()
	for key, value := range msg.Payload {
		switch {
		case strings.HasPrefix(key, "drone_status_"):
			dID := strings.TrimPrefix(key, "drone_status_")
			if drones[dID] == "" {
				drones[dID] = value
			}
		case strings.HasPrefix(key, "drone_addr_"):
			dID := strings.TrimPrefix(key, "drone_addr_")
			if droneAddrs[dID] == "" {
				droneAddrs[dID] = value
			}
		}
	}
	log.Printf("[GATEWAY/%s] Estado sincronizado. Drones conhecidos: %v", gatewayID, len(drones))
}

// ---- ALGORITMO RICART-AGRAWALA MODIFICADO ----
func handleRARequest(msg Message, conn net.Conn) {
	stateMutex.Lock()
	defer stateMutex.Unlock()

	droneID := msg.DroneID
	inCS := (droneOwners[droneID] == gatewayID)
	wantCS := requestingCS[droneID]
	myReq := myCurrentReq[droneID]

	// Regras de prioridade e adiamento
	deferReply := false
	if inCS {
		deferReply = true
	} else if wantCS {
		if myReq.Priority < msg.Priority {
			deferReply = true
		} else if myReq.Priority == msg.Priority {
			if myReq.Lamport < msg.Lamport {
				deferReply = true
			} else if myReq.Lamport == msg.Lamport {
				if gatewayID < msg.GatewayID {
					deferReply = true
				}
			}
		}
	}

	if deferReply {
		deferred[droneID] = append(deferred[droneID], msg)
		log.Printf("[R-A] REQUEST adiado para %s sobre o drone %s", msg.GatewayID, droneID)
	} else {
		replyMsg := Message{
			Type:      MsgReply,
			DroneID:   droneID,
			GatewayID: gatewayID,
			Lamport:   tickLamport(0),
		}
		go sendDirect(msg.GatewayID, replyMsg) // Precisaria mapear GatewayID pra IP. Simplificação: Broadcast Reply
		broadcastPeerMsg(replyMsg)
	}
}

func handleRAReply(msg Message) {
	stateMutex.Lock()
	defer stateMutex.Unlock()
	if requestingCS[msg.DroneID] {
		repliesCount[msg.DroneID]++
		// Espera 3 replies (assumindo 4 setores). Em um cenário real com tolerância a falhas, usa timeout.
		if repliesCount[msg.DroneID] >= len(peers)-1 {
			requestingCS[msg.DroneID] = false
			droneOwners[msg.DroneID] = gatewayID
			drones[msg.DroneID] = DroneBusy
			log.Printf("[R-A] Região Crítica alcançada para drone %s!", msg.DroneID)
			go dispatchDrone(msg.DroneID)
		}
	}
}

func handleRARelease(msg Message) {
	stateMutex.Lock()
	defer stateMutex.Unlock()
	drones[msg.DroneID] = DroneAvailable
	droneOwners[msg.DroneID] = ""
	log.Printf("[R-A] Drone %s liberado pelo setor %s", msg.DroneID, msg.GatewayID)
}

func releaseCS(droneID string) {
	stateMutex.Lock()
	drones[droneID] = DroneAvailable
	droneOwners[droneID] = ""
	defList := deferred[droneID]
	deferred[droneID] = []Message{}
	stateMutex.Unlock()

	// Avisa a malha que soltou
	msg := Message{Type: MsgRelease, DroneID: droneID, GatewayID: gatewayID, Lamport: tickLamport(0)}
	broadcastPeerMsg(msg)

	// Responde aos adiados
	for _, req := range defList {
		reply := Message{Type: MsgReply, DroneID: droneID, GatewayID: gatewayID, Lamport: tickLamport(0)}
		if req.GatewayID != "" {
			log.Printf("[R-A] Enviando REPLY deferred para %s sobre drone %s", req.GatewayID, droneID)
		}
		broadcastPeerMsg(reply) // Simplificação de envio
	}
}

// ---- LÓGICA DE FILA E DESPACHO ----
func processQueueLoop() {
	for {
		time.Sleep(1 * time.Second)
		stateMutex.Lock()
		if reqQueue.Len() > 0 {
			// Procura um drone disponível na malha que já esteja registrado com endereço
			var targetDrone string
			for dID, status := range drones {
				if status == DroneAvailable && droneAddrs[dID] != "" {
					targetDrone = dID
					break
				}
			}

			if targetDrone == "" {
				log.Printf("[GATEWAY/%s] Nenhum drone disponível e registrado. Aguardando disponibilidade.", gatewayID)
				stateMutex.Unlock()
				continue
			}

			if !requestingCS[targetDrone] {
				req := heap.Pop(&reqQueue).(*AlertRequest)
				log.Printf("[GATEWAY/%s] Iniciando R-A para ocorrência %s no drone %s", gatewayID, req.Occurrence, targetDrone)

				requestingCS[targetDrone] = true
				repliesCount[targetDrone] = 0
				myCurrentReq[targetDrone] = Message{
					Type: MsgRequest, DroneID: targetDrone, GatewayID: gatewayID,
					Priority: req.Priority, Lamport: req.Lamport,
				}

				stateMutex.Unlock()
				msg := myCurrentReq[targetDrone]
				msg.Lamport = tickLamport(0)
				broadcastPeerMsg(msg)
				continue
			}
		}
		stateMutex.Unlock()
	}
}

func dispatchDrone(droneID string) {
	addr := droneAddrs[droneID]
	if addr == "" {
		log.Printf("[FALHA] Endereço do drone %s desconhecido.", droneID)
		simulateDroneFailure(droneID)
		return
	}
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		log.Printf("[FALHA] Não foi possível conectar ao drone %s", droneID)
		simulateDroneFailure(droneID)
		return
	}
	defer conn.Close()
	msg := Message{Type: "DISPATCH", GatewayID: gatewayID, Lamport: tickLamport(0)}
	json.NewEncoder(conn).Encode(msg)
}

func handleDroneFailed(msg Message) {
	stateMutex.Lock()
	defer stateMutex.Unlock()
	drones[msg.DroneID] = DroneFailed
	droneOwners[msg.DroneID] = ""
	log.Printf("[FALHA] Broadcast recebido: Drone %s falhou.", msg.DroneID)
}

func simulateDroneFailure(droneID string) {
	// Manda falha pra malha e recoloca tarefa na fila (simplificado)
	msg := Message{Type: MsgDroneFailed, DroneID: droneID, GatewayID: gatewayID, Lamport: tickLamport(0)}
	broadcastPeerMsg(msg)
	releaseCS(droneID)
}

// ---- APIS DE REGISTRO E CLIENTE ----
func handleRegConnection(conn net.Conn) {
	defer conn.Close()
	var msg Message
	json.NewDecoder(conn).Decode(&msg)
	if msg.Type == MsgDeviceReg {
		stateMutex.Lock()
		drones[msg.DroneID] = DroneAvailable
		droneAddrs[msg.DroneID] = msg.Content // Contém IP:Porta do drone
		stateMutex.Unlock()
		log.Printf("[DRONE] Registrado: %s em %s", msg.DroneID, msg.Content)
		broadcastDroneState()
	} else if msg.Type == MsgRelease {
		releaseCS(msg.DroneID)
	}
}

func broadcastDroneState() {
	stateMutex.Lock()
	payload := make(map[string]string)
	for dID, status := range drones {
		payload["drone_status_"+dID] = status
	}
	for dID, addr := range droneAddrs {
		payload["drone_addr_"+dID] = addr
	}
	stateMutex.Unlock()
	msg := Message{
		Type:      MsgStateSync,
		GatewayID: gatewayID,
		Lamport:   tickLamport(0),
		Payload:   payload,
	}
	broadcastPeerMsg(msg)
}

func handleClientConnection(conn net.Conn) {
	defer conn.Close()
	var msg Message
	json.NewDecoder(conn).Decode(&msg)

	if msg.Type == MsgAlert {
		log.Printf("[BEACON/CLIENT] Alerta recebido: Prio %d, Ocorrencia: %s", msg.Priority, msg.Occurrence)
		stateMutex.Lock()
		heap.Push(&reqQueue, &AlertRequest{
			Occurrence: msg.Occurrence,
			Priority:   msg.Priority,
			Lamport:    tickLamport(0),
			GatewayID:  gatewayID,
			Timestamp:  time.Now().Unix(),
		})
		stateMutex.Unlock()
	} else if msg.Type == MsgStatusReq {
		stateMutex.Lock()
		payload := map[string]string{
			"gateway_id": gatewayID,
			"queue_size": fmt.Sprintf("%d", reqQueue.Len()),
		}
		for dID, st := range drones {
			payload["drone_"+dID] = st
		}
		stateMutex.Unlock()
		json.NewEncoder(conn).Encode(Message{Type: "STATUS_REP", Payload: payload})
	}
}

func sendDirect(targetGateway string, msg Message) {
	// Em um ambiente real, mapear GatewayID para IP_SUL, IP_NORTE, etc.
}
