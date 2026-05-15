package main

import (
	"container/heap"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

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
	MsgEventsReq       = "EVENTS_REQ"
	MsgEventsRep       = "EVENTS_REP"
	MsgStatusRep       = "STATUS_REP"
)

const (
	DroneAvailable = "DISPONIVEL"
	DroneBusy      = "OCUPADO"
	DroneFailed    = "FALHO"
)

type Message struct {
	Type        string            `json:"type"`
	DroneID     string            `json:"drone_id,omitempty"`
	GatewayID   string            `json:"gateway_id,omitempty"`
	Priority    int               `json:"priority,omitempty"`
	Lamport     int               `json:"lamport,omitempty"`
	Timestamp   int64             `json:"timestamp,omitempty"`
	Payload     map[string]string `json:"payload,omitempty"`
	Content     string            `json:"content,omitempty"`
	Status      string            `json:"status,omitempty"`
	MissionInfo string            `json:"mission_info,omitempty"`
	Occurrence  string            `json:"occurrence,omitempty"`
}

type DroneState struct {
	ID            string
	Status        string
	GatewayAtual  string
	ControlAddr   string
	MissionActive bool
	MissionInfo   string
	LastHeartbeat time.Time
	SetorBase     string
	LastUpdate    time.Time
}

type AlertRequest struct {
	Occurrence string
	Priority   int
	Lamport    int
	GatewayID  string
	Timestamp  int64
}

type PriorityQueue []*AlertRequest

func (pq PriorityQueue) Len() int { return len(pq) }
func (pq PriorityQueue) Less(i, j int) bool {
	if pq[i].Priority != pq[j].Priority {
		return pq[i].Priority < pq[j].Priority
	}
	if pq[i].Lamport != pq[j].Lamport {
		return pq[i].Lamport < pq[j].Lamport
	}
	return pq[i].GatewayID < pq[j].GatewayID
}
func (pq PriorityQueue) Swap(i, j int)       { pq[i], pq[j] = pq[j], pq[i] }
func (pq *PriorityQueue) Push(x interface{}) { *pq = append(*pq, x.(*AlertRequest)) }
func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[:n-1]
	return item
}

var (
	gatewayID         string
	gatewayIP         string
	gatewayHost       string
	regPort           string
	clientPort        string
	peerPort          string
	peerAddrsByID     map[string]string
	peers             []string
	peerIDs           []string
	peerOfflineUntil  = make(map[string]time.Time)
	replyChannels     = make(map[string]map[string]chan struct{})
	replyChannelMutex sync.Mutex
	lamportClock      int
	lamportMutex      sync.Mutex
	stateMutex        sync.Mutex
	drones            = make(map[string]*DroneState)
	activeBeacons     = make(map[string]time.Time)
	droneOwners       = make(map[string]string)
	deferred          = make(map[string][]Message)
	repliesCount      = make(map[string]int)
	requestingCS      = make(map[string]bool)
	myCurrentReq      = make(map[string]Message)
	reqQueue          PriorityQueue
	eventLog          []string
	eventMutex        sync.Mutex
)

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("[FATAL] Variável de ambiente obrigatória ausente: %s", key)
	}
	return v
}

func main() {
	heap.Init(&reqQueue)
	gatewayID = mustEnv("GATEWAY_ID")
	gatewayIP = mustEnv("GATEWAY_IP")
	gatewayHost = mustEnv("GATEWAY_HOST")
	regPort = mustEnv("GATEWAY_TCP_REG_PORT")
	clientPort = mustEnv("GATEWAY_TCP_CLIENT_PORT")
	peerPort = mustEnv("GATEWAY_TCP_PEER_PORT")

	peerAddrsByID = map[string]string{
		"Norte": fmt.Sprintf("%s:%s", mustEnv("IP_NORTE"), peerPort),
		"Sul":   fmt.Sprintf("%s:%s", mustEnv("IP_SUL"), peerPort),
		"Leste": fmt.Sprintf("%s:%s", mustEnv("IP_LESTE"), peerPort),
		"Oeste": fmt.Sprintf("%s:%s", mustEnv("IP_OESTE"), peerPort),
	}

	for id, addr := range peerAddrsByID {
		if id == gatewayID {
			continue
		}
		peers = append(peers, addr)
		peerIDs = append(peerIDs, id)
	}

	log.Printf("[GATEWAY/%s] Iniciando gateway em %s", gatewayID, gatewayHost)
	log.Printf("[GATEWAY/%s] Peers conhecidos: %v", gatewayID, peers)

	go startServer(gatewayHost, peerPort, handlePeerConnection)
	go startServer(gatewayHost, regPort, handleRegConnection)
	go startServer(gatewayHost, clientPort, handleClientConnection)

	go syncStateOnStart()
	go processQueueLoop()
	go monitorLocalDroneHeartbeats()

	select {}
}

func startServer(host, port string, handler func(net.Conn)) {
	addr := fmt.Sprintf("%s:%s", host, port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("[GATEWAY/%s] Falha ao escutar em %s: %v", gatewayID, addr, err)
	}
	log.Printf("[GATEWAY/%s] Servidor TCP ativo em %s", gatewayID, addr)
	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go handler(conn)
	}
}

func handlePeerConnection(conn net.Conn) {
	defer conn.Close()
	var msg Message
	if err := json.NewDecoder(conn).Decode(&msg); err != nil {
		return
	}
	updateLamport(msg.Lamport)
	switch msg.Type {
	case MsgRequest:
		handleRARequest(msg)
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

func handleRegConnection(conn net.Conn) {
	defer conn.Close()
	var msg Message
	if err := json.NewDecoder(conn).Decode(&msg); err != nil {
		return
	}
	updateLamport(msg.Lamport)
	switch msg.Type {
	case MsgDeviceReg:
		handleDeviceRegistration(msg)
	case MsgHeartbeat:
		handleDroneHeartbeat(msg)
	case MsgRelease:
		releaseCS(msg.DroneID, true)
	case MsgAlert:
		enqueueAlert(msg)
	case MsgStatusReq:
		sendStatusRep(conn)
	case MsgEventsReq:
		sendEventsRep(conn, msg)
	}
}

func handleClientConnection(conn net.Conn) {
	defer conn.Close()
	var msg Message
	if err := json.NewDecoder(conn).Decode(&msg); err != nil {
		return
	}
	switch msg.Type {
	case MsgAlert:
		enqueueAlert(msg)
	case MsgStatusReq:
		sendStatusRep(conn)
	case MsgEventsReq:
		sendEventsRep(conn, msg)
	}
}

func enqueueAlert(msg Message) {
	stateMutex.Lock()
	defer stateMutex.Unlock()
	req := &AlertRequest{
		Occurrence: msg.Occurrence,
		Priority:   msg.Priority,
		Lamport:    tickLamport(msg.Lamport),
		GatewayID:  gatewayID,
		Timestamp:  time.Now().Unix(),
	}
	heap.Push(&reqQueue, req)
	logEvent(fmt.Sprintf("[R-A] Alerta enfileirado: %s prior. %d", req.Occurrence, req.Priority))
	log.Printf("[GATEWAY/%s] [R-A] Alerta enfileirado: %s prioridade %d", gatewayID, req.Occurrence, req.Priority)
}

func handleDeviceRegistration(msg Message) {
	stateMutex.Lock()
	drone, exists := drones[msg.DroneID]
	if !exists {
		drone = &DroneState{ID: msg.DroneID}
		drones[msg.DroneID] = drone
	}
	drone.ControlAddr = msg.Content
	drone.GatewayAtual = gatewayID
	drone.Status = msg.Status
	drone.MissionActive = strings.EqualFold(msg.Status, DroneBusy)
	drone.MissionInfo = msg.MissionInfo
	drone.LastHeartbeat = time.Now()
	drone.LastUpdate = time.Now()
	activeBeacons[msg.DroneID] = drone.LastHeartbeat
	stateMutex.Unlock()

	logEvent(fmt.Sprintf("[DRONE] Drone %s registrado com status %s", msg.DroneID, msg.Status))
	log.Printf("[GATEWAY/%s] [DRONE] Drone %s registrado. Status: %s", gatewayID, msg.DroneID, msg.Status)
}

func handleDroneHeartbeat(msg Message) {
	stateMutex.Lock()
	drone, exists := drones[msg.DroneID]
	if !exists {
		drone = &DroneState{ID: msg.DroneID}
		drones[msg.DroneID] = drone
	}
	if msg.Content != "" {
		drone.ControlAddr = msg.Content
	}
	drone.GatewayAtual = gatewayID
	drone.Status = msg.Status
	drone.MissionActive = strings.EqualFold(msg.Status, DroneBusy)
	drone.MissionInfo = msg.MissionInfo
	drone.LastHeartbeat = time.Now()
	drone.LastUpdate = time.Now()
	activeBeacons[msg.DroneID] = drone.LastHeartbeat
	stateMutex.Unlock()

	logEvent(fmt.Sprintf("[HEARTBEAT] Drone %s heartbeat recebido", msg.DroneID))
	log.Printf("[GATEWAY/%s] [HEARTBEAT] Drone %s heartbeat recebido", gatewayID, msg.DroneID)
}

func sendStatusRep(conn net.Conn) {
	stateMutex.Lock()
	payload := map[string]string{}
	payload["gateway_id"] = gatewayID
	payload["queue_size"] = fmt.Sprintf("%d", reqQueue.Len())
	for _, drone := range drones {
		keyPrefix := fmt.Sprintf("drone_%s_", drone.ID)
		payload[keyPrefix+"status"] = drone.Status
		payload[keyPrefix+"gateway_atual"] = drone.GatewayAtual
		payload[keyPrefix+"control_addr"] = drone.ControlAddr
		payload[keyPrefix+"mission_active"] = fmt.Sprintf("%t", drone.MissionActive)
		payload[keyPrefix+"mission_info"] = drone.MissionInfo
		payload[keyPrefix+"ultima_atualizacao"] = fmt.Sprintf("%d", drone.LastUpdate.Unix())
	}
	stateMutex.Unlock()
	json.NewEncoder(conn).Encode(Message{Type: MsgStatusRep, Payload: payload})
}

func sendEventsRep(conn net.Conn, msg Message) {
	count := 5
	if msg.Payload != nil {
		if s, ok := msg.Payload["count"]; ok {
			if v, err := strconv.Atoi(s); err == nil && v > 0 {
				count = v
			}
		}
	}
	eventMutex.Lock()
	events := append([]string(nil), eventLog...)
	eventMutex.Unlock()

	payload := map[string]string{}
	for i := 0; i < count && i < len(events); i++ {
		payload[fmt.Sprintf("event_%d", i+1)] = events[i]
	}
	json.NewEncoder(conn).Encode(Message{Type: MsgEventsRep, Payload: payload})
}

func handleRARequest(msg Message) {
	stateMutex.Lock()
	defer stateMutex.Unlock()
	droneID := msg.DroneID
	inCS := droneOwners[droneID] == gatewayID
	wantCS := requestingCS[droneID]
	myReq := myCurrentReq[droneID]
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
		event := fmt.Sprintf("[R-A] REQUEST adiado para drone %s de %s", droneID, msg.GatewayID)
		logEvent(event)
		log.Printf("[GATEWAY/%s] [R-A] %s", gatewayID, event)
		return
	}

	reply := Message{Type: MsgReply, DroneID: droneID, GatewayID: gatewayID, Lamport: tickLamport(0)}
	sendDirect(msg.GatewayID, reply)
}

func handleRAReply(msg Message) {
	stateMutex.Lock()
	if !requestingCS[msg.DroneID] {
		stateMutex.Unlock()
		return
	}
	repliesCount[msg.DroneID]++
	stateMutex.Unlock()
	log.Printf("[GATEWAY/%s] [R-A] Reply recebido para drone %s de %s", gatewayID, msg.DroneID, msg.GatewayID)
	if ch := getReplyChannel(msg.DroneID, msg.GatewayID); ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func handleRARelease(msg Message) {
	stateMutex.Lock()
	if drone, ok := drones[msg.DroneID]; ok {
		drone.Status = DroneAvailable
		drone.MissionActive = false
		drone.MissionInfo = ""
		drone.GatewayAtual = msg.GatewayID
		drone.LastUpdate = time.Now()
	}
	droneOwners[msg.DroneID] = ""
	stateMutex.Unlock()
	log.Printf("[GATEWAY/%s] [R-A] Drone %s liberado por %s", gatewayID, msg.DroneID, msg.GatewayID)
}

func releaseCS(droneID string, available bool) {
	stateMutex.Lock()
	deferredList := deferred[droneID]
	deferred[droneID] = nil
	req := myCurrentReq[droneID]
	myCurrentReq[droneID] = Message{}
	requestingCS[droneID] = false
	repliesCount[droneID] = 0
	if available {
		droneOwners[droneID] = ""
		if drone, ok := drones[droneID]; ok {
			drone.Status = DroneAvailable
			drone.MissionActive = false
			drone.MissionInfo = ""
			drone.LastUpdate = time.Now()
		}
	}
	stateMutex.Unlock()

	if available {
		broadcastPeerMsg(Message{Type: MsgRelease, DroneID: droneID, GatewayID: gatewayID, Lamport: tickLamport(0)})
	}

	for _, pending := range deferredList {
		reply := Message{Type: MsgReply, DroneID: droneID, GatewayID: gatewayID, Lamport: tickLamport(0)}
		sendDirect(pending.GatewayID, reply)
	}

	if !available && req.Type != "" && req.GatewayID == gatewayID {
		stateMutex.Lock()
		heap.Push(&reqQueue, &AlertRequest{Occurrence: req.Occurrence, Priority: req.Priority, Lamport: req.Lamport, GatewayID: req.GatewayID, Timestamp: req.Timestamp})
		stateMutex.Unlock()
		logEvent(fmt.Sprintf("[R-A] Reenfileirando requisição do drone %s após falha", droneID))
		log.Printf("[GATEWAY/%s] [R-A] Reenfileirando requisição do drone %s após falha", gatewayID, droneID)
	}
}

func processQueueLoop() {
	for {
		time.Sleep(1 * time.Second)
		stateMutex.Lock()
		if reqQueue.Len() == 0 {
			stateMutex.Unlock()
			continue
		}

		var targetDrone string
		for _, drone := range drones {
			if drone.Status == DroneAvailable && drone.ControlAddr != "" {
				targetDrone = drone.ID
				break
			}
		}

		if targetDrone == "" {
			stateMutex.Unlock()
			continue
		}

		if !requestingCS[targetDrone] {
			req := heap.Pop(&reqQueue).(*AlertRequest)
			logEvent(fmt.Sprintf("[R-A] Iniciando R-A para drone %s com prioridade %d", targetDrone, req.Priority))
			log.Printf("[GATEWAY/%s] [R-A] Iniciando R-A para drone %s com prioridade %d", gatewayID, targetDrone, req.Priority)
			requestingCS[targetDrone] = true
			repliesCount[targetDrone] = 0
			myCurrentReq[targetDrone] = Message{Type: MsgRequest, DroneID: targetDrone, GatewayID: gatewayID, Priority: req.Priority, Lamport: req.Lamport, Occurrence: req.Occurrence}
			stateMutex.Unlock()

			msg := myCurrentReq[targetDrone]
			msg.Lamport = tickLamport(0)
			go waitForReplies(targetDrone, msg)
			continue
		}
		stateMutex.Unlock()
	}
}

func waitForReplies(droneID string, msg Message) {
	stateMutex.Lock()
	if !requestingCS[droneID] {
		stateMutex.Unlock()
		return
	}
	activePeers := []string{}
	for _, peerID := range peerIDs {
		if time.Now().Before(peerOfflineUntil[peerID]) {
			continue
		}
		activePeers = append(activePeers, peerID)
	}
	stateMutex.Unlock()

	if len(activePeers) == 0 {
		stateMutex.Lock()
		requestingCS[droneID] = false
		droneOwners[droneID] = gatewayID
		if drone, ok := drones[droneID]; ok {
			drone.Status = DroneBusy
			drone.MissionActive = true
		}
		stateMutex.Unlock()
		logEvent(fmt.Sprintf("[R-A] Região crítica obtida para drone %s sem peers ativos", droneID))
		log.Printf("[GATEWAY/%s] [R-A] Região crítica obtida para drone %s sem peers ativos", gatewayID, droneID)
		go dispatchDrone(droneID)
		return
	}

	replyChannelMutex.Lock()
	replyChannels[droneID] = make(map[string]chan struct{}, len(activePeers))
	for _, peerID := range activePeers {
		replyChannels[droneID][peerID] = make(chan struct{}, 1)
	}
	replyChannelMutex.Unlock()

	for _, peerID := range activePeers {
		sendDirect(peerID, msg)
	}

	gotReplies := 0
	for _, peerID := range activePeers {
		ch := getReplyChannel(droneID, peerID)
		if ch == nil {
			continue
		}
		select {
		case <-ch:
			gotReplies++
		case <-time.After(4 * time.Second):
			stateMutex.Lock()
			peerOfflineUntil[peerID] = time.Now().Add(15 * time.Second)
			stateMutex.Unlock()
			log.Printf("[GATEWAY/%s] [R-A] Peer sem resposta ou offline: %s", gatewayID, peerID)
		}
	}

	replyChannelMutex.Lock()
	delete(replyChannels, droneID)
	replyChannelMutex.Unlock()

	stateMutex.Lock()
	if requestingCS[droneID] && gotReplies == len(activePeers) {
		requestingCS[droneID] = false
		droneOwners[droneID] = gatewayID
		if drone, ok := drones[droneID]; ok {
			drone.Status = DroneBusy
			drone.MissionActive = true
		}
		stateMutex.Unlock()
		logEvent(fmt.Sprintf("[R-A] Região crítica obtida para drone %s", droneID))
		log.Printf("[GATEWAY/%s] [R-A] Região crítica obtida para drone %s", gatewayID, droneID)
		go dispatchDrone(droneID)
		return
	}
	if requestingCS[droneID] {
		requestingCS[droneID] = false
		if req, ok := myCurrentReq[droneID]; ok {
			heap.Push(&reqQueue, &AlertRequest{Occurrence: req.Occurrence, Priority: req.Priority, Lamport: req.Lamport, GatewayID: req.GatewayID, Timestamp: time.Now().Unix()})
		}
	}
	stateMutex.Unlock()
}

func dispatchDrone(droneID string) {
	stateMutex.Lock()
	drone, ok := drones[droneID]
	stateMutex.Unlock()
	if !ok || drone.ControlAddr == "" {
		log.Printf("[GATEWAY/%s] [FALHA] Endereço do drone %s desconhecido", gatewayID, droneID)
		handleLocalDroneFailure(droneID, "endereço desconhecido")
		return
	}

	conn, err := net.DialTimeout("tcp", drone.ControlAddr, 3*time.Second)
	if err != nil {
		log.Printf("[GATEWAY/%s] [FALHA] Não foi possível conectar ao drone %s: %v", gatewayID, droneID, err)
		handleLocalDroneFailure(droneID, "conexão falhou")
		return
	}
	defer conn.Close()

	msg := Message{Type: "DISPATCH", DroneID: droneID, GatewayID: gatewayID, Lamport: tickLamport(0)}
	if err := json.NewEncoder(conn).Encode(msg); err != nil {
		log.Printf("[GATEWAY/%s] [FALHA] Erro ao enviar DISPATCH ao drone %s: %v", gatewayID, droneID, err)
		handleLocalDroneFailure(droneID, "envio DISPATCH falhou")
		return
	}

	log.Printf("[GATEWAY/%s] [DESPACHO] Drone %s despachado com sucesso", gatewayID, droneID)
}

func handleDroneFailed(msg Message) {
	stateMutex.Lock()
	drone, ok := drones[msg.DroneID]
	if ok {
		drone.Status = DroneFailed
		drone.MissionActive = false
		drone.MissionInfo = "falha detectada"
		drone.LastUpdate = time.Now()
	}
	wasOwner := droneOwners[msg.DroneID] == gatewayID
	droneOwners[msg.DroneID] = ""
	stateMutex.Unlock()

	logEvent(fmt.Sprintf("[FALHA] Drone %s marcado como FALHO", msg.DroneID))
	log.Printf("[GATEWAY/%s] [FALHA] Drone %s marcado como FALHO por broadcast", gatewayID, msg.DroneID)
	if wasOwner {
		releaseCS(msg.DroneID, false)
	}
}

func handleLocalDroneFailure(droneID, reason string) {
	stateMutex.Lock()
	drone, ok := drones[droneID]
	if ok {
		drone.Status = DroneFailed
		drone.MissionActive = false
		drone.MissionInfo = reason
		drone.LastUpdate = time.Now()
	}
	currentReq := myCurrentReq[droneID]
	downOwner := droneOwners[droneID] == gatewayID
	droneOwners[droneID] = ""
	requestingCS[droneID] = false
	repliesCount[droneID] = 0
	myCurrentReq[droneID] = Message{}
	stateMutex.Unlock()

	logEvent(fmt.Sprintf("[FALHA] Drone %s falhou localmente: %s", droneID, reason))
	log.Printf("[GATEWAY/%s] [FALHA] Drone %s falhou localmente: %s", gatewayID, droneID, reason)
	broadcastPeerMsg(Message{Type: MsgDroneFailed, DroneID: droneID, GatewayID: gatewayID, Lamport: tickLamport(0)})

	if downOwner && currentReq.Type != "" {
		stateMutex.Lock()
		heap.Push(&reqQueue, &AlertRequest{Occurrence: currentReq.Occurrence, Priority: currentReq.Priority, Lamport: currentReq.Lamport, GatewayID: currentReq.GatewayID, Timestamp: currentReq.Timestamp})
		stateMutex.Unlock()
		logEvent(fmt.Sprintf("[R-A] Reenfileirando requisição do drone %s após falha", droneID))
		log.Printf("[GATEWAY/%s] [R-A] Reenfileirando requisição do drone %s após falha", gatewayID, droneID)
	}
}

func syncStateOnStart() {
	time.Sleep(2 * time.Second)
	msg := Message{Type: MsgSnapshotRequest, GatewayID: gatewayID, Lamport: tickLamport(0)}
	broadcastPeerMsg(msg)
}

func sendStateSync(conn net.Conn) {
	stateMutex.Lock()
	payload := make(map[string]string)
	for _, drone := range drones {
		base := fmt.Sprintf("drone_%s_", drone.ID)
		payload[base+"status"] = drone.Status
		payload[base+"gateway_atual"] = drone.GatewayAtual
		payload[base+"control_addr"] = drone.ControlAddr
		payload[base+"mission_active"] = fmt.Sprintf("%t", drone.MissionActive)
		payload[base+"mission_info"] = drone.MissionInfo
		payload[base+"ultimo_heartbeat"] = fmt.Sprintf("%d", drone.LastHeartbeat.UnixNano())
		payload[base+"ultima_atualizacao"] = fmt.Sprintf("%d", drone.LastUpdate.UnixNano())
		payload[base+"setor_base"] = drone.SetorBase
	}
	stateMutex.Unlock()
	json.NewEncoder(conn).Encode(Message{Type: MsgStateSync, GatewayID: gatewayID, Lamport: tickLamport(0), Payload: payload})
}

func receiveStateSync(msg Message) {
	stateMutex.Lock()
	for key, value := range msg.Payload {
		if !strings.HasPrefix(key, "drone_") {
			continue
		}
		remainder := strings.TrimPrefix(key, "drone_")
		last := strings.LastIndex(remainder, "_")
		if last <= 0 {
			continue
		}
		droneID := remainder[:last]
		field := remainder[last+1:]
		drone, ok := drones[droneID]
		if !ok {
			drone = &DroneState{ID: droneID}
			drones[droneID] = drone
		}
		update := time.Now()
		switch field {
		case "status":
			drone.Status = value
		case "gateway_atual":
			drone.GatewayAtual = value
		case "control_addr":
			drone.ControlAddr = value
		case "mission_active":
			drone.MissionActive = value == "true"
		case "mission_info":
			drone.MissionInfo = value
		case "ultimo_heartbeat":
			if ts, err := strconv.ParseInt(value, 10, 64); err == nil {
				drone.LastHeartbeat = time.Unix(0, ts)
			}
		case "ultima_atualizacao":
			if ts, err := strconv.ParseInt(value, 10, 64); err == nil {
				update = time.Unix(0, ts)
			}
			drone.LastUpdate = update
		case "setor_base":
			drone.SetorBase = value
		}
	}
	stateMutex.Unlock()
	log.Printf("[GATEWAY/%s] [SYNC] Estado sincronizado recebido de %s", gatewayID, msg.GatewayID)
}

func monitorLocalDroneHeartbeats() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		stateMutex.Lock()
		localCopy := make(map[string]struct {
			last    time.Time
			gateway string
			status  string
		})
		for id, drone := range drones {
			localCopy[id] = struct {
				last    time.Time
				gateway string
				status  string
			}{last: drone.LastHeartbeat, gateway: drone.GatewayAtual, status: drone.Status}
		}
		stateMutex.Unlock()

		for droneID, info := range localCopy {
			if time.Since(info.last) > 15*time.Second {
				if info.gateway == gatewayID && info.status != DroneFailed {
					handleLocalDroneFailure(droneID, "heartbeat ausente")
				}
			}
		}
	}
}

func broadcastPeerMsg(msg Message) {
	for _, peer := range peers {
		go func(p string) {
			conn, err := net.DialTimeout("tcp", p, 2*time.Second)
			if err != nil {
				return
			}
			defer conn.Close()
			json.NewEncoder(conn).Encode(msg)
		}(peer)
	}
}

func sendDirect(targetGateway string, msg Message) {
	addr, ok := peerAddrsByID[targetGateway]
	if !ok {
		return
	}
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return
	}
	defer conn.Close()
	json.NewEncoder(conn).Encode(msg)
}

func getReplyChannel(droneID, peerID string) chan struct{} {
	replyChannelMutex.Lock()
	defer replyChannelMutex.Unlock()
	if peerMap, ok := replyChannels[droneID]; ok {
		return peerMap[peerID]
	}
	return nil
}

func tickLamport(recv int) int {
	lamportMutex.Lock()
	defer lamportMutex.Unlock()
	if recv > lamportClock {
		lamportClock = recv
	}
	lamportClock++
	return lamportClock
}

func updateLamport(recv int) {
	lamportMutex.Lock()
	defer lamportMutex.Unlock()
	if recv > lamportClock {
		lamportClock = recv
	}
	lamportClock++
}

func logEvent(event string) {
	eventMutex.Lock()
	defer eventMutex.Unlock()
	if len(eventLog) >= 100 {
		eventLog = eventLog[1:]
	}
	eventLog = append(eventLog, fmt.Sprintf("%s %s", time.Now().Format(time.RFC3339), event))
}
