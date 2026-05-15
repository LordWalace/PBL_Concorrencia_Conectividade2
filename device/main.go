package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

type Message struct {
	Type        string            `json:"type"`
	DroneID     string            `json:"drone_id"`
	Content     string            `json:"content,omitempty"`
	Status      string            `json:"status,omitempty"`
	MissionInfo string            `json:"mission_info,omitempty"`
	Payload     map[string]string `json:"payload,omitempty"`
}

var (
	droneID            string
	deviceIP           string
	deviceHost         string
	deviceControlPort  string
	failureRate        float64
	gatewayAddrs       []string
	gatewayNames       []string
	stateMutex         sync.Mutex
	currentGatewayIdx  int = -1
	currentGateway     string
	currentGatewayName string
	statusValue        string = "DISPONIVEL"
	missionActive      bool
	missionEnd         time.Time
	lamportClock       int
	clockMutex         sync.Mutex
)

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("[FATAL] Variável de ambiente obrigatória ausente: %s", key)
	}
	return v
}

func main() {
	droneID = mustEnv("DEVICE_ID")
	deviceIP = mustEnv("DEVICE_IP")
	deviceHost = mustEnv("DEVICE_HOST")
	deviceControlPort = mustEnv("DEVICE_CONTROL_PORT")
	failureRate = parseFailureRate(mustEnv("FAILURE_RATE"))

	gatewayNames = []string{"Norte", "Sul", "Leste", "Oeste"}
	gatewayAddrs = []string{
		fmt.Sprintf("%s:%s", mustEnv("IP_NORTE"), mustEnv("GATEWAY_TCP_REG_PORT")),
		fmt.Sprintf("%s:%s", mustEnv("IP_SUL"), mustEnv("GATEWAY_TCP_REG_PORT")),
		fmt.Sprintf("%s:%s", mustEnv("IP_LESTE"), mustEnv("GATEWAY_TCP_REG_PORT")),
		fmt.Sprintf("%s:%s", mustEnv("IP_OESTE"), mustEnv("GATEWAY_TCP_REG_PORT")),
	}

	logPrefix := fmt.Sprintf("[DRONE/%s]", droneID)
	log.Printf("%s Iniciando device", logPrefix)

	preferredIndex := locatePreferredGateway(deviceIP)
	if preferredIndex >= 0 {
		log.Printf("%s Gateway preferencial identificado: %s (%s)", logPrefix, gatewayNames[preferredIndex], gatewayAddrs[preferredIndex])
	}

	myControlAddr := fmt.Sprintf("%s:%s", deviceHost, deviceControlPort)

	go registerLoop(myControlAddr, preferredIndex)
	go heartbeatLoop(myControlAddr)
	startCommandListener(myControlAddr)
}

func parseFailureRate(value string) float64 {
	rate, err := strconv.ParseFloat(value, 64)
	if err != nil || rate < 0 || rate > 1 {
		log.Fatalf("[FATAL] FAILURE_RATE inválido: %s", value)
	}
	return rate
}

func locatePreferredGateway(deviceIP string) int {
	ips := []string{mustEnv("IP_NORTE"), mustEnv("IP_SUL"), mustEnv("IP_LESTE"), mustEnv("IP_OESTE")}
	for i, ip := range ips {
		if ip == deviceIP {
			return i
		}
	}
	return 0
}

func registerLoop(controlAddr string, preferredIndex int) {
	logPrefix := fmt.Sprintf("[DRONE/%s]", droneID)
	for {
		if registerToGateway(controlAddr, preferredIndex) == nil {
			return
		}
		log.Printf("%s Registro inicial falhou, tentando novamente em 5s", logPrefix)
		time.Sleep(5 * time.Second)
	}
}

func registerToGateway(controlAddr string, preferredIndex int) error {
	logPrefix := fmt.Sprintf("[DRONE/%s]", droneID)
	order := gatewayOrder(preferredIndex)
	for _, idx := range order {
		if tryRegisterGateway(idx, controlAddr) == nil {
			return nil
		}
	}
	log.Printf("%s Nenhum gateway disponível para registro", logPrefix)
	return fmt.Errorf("nenhum gateway disponível")
}

func gatewayOrder(preferredIndex int) []int {
	order := make([]int, 0, len(gatewayAddrs))
	if preferredIndex < 0 || preferredIndex >= len(gatewayAddrs) {
		preferredIndex = 0
	}
	for i := 0; i < len(gatewayAddrs); i++ {
		order = append(order, (preferredIndex+i)%len(gatewayAddrs))
	}
	return order
}

func tryRegisterGateway(idx int, controlAddr string) error {
	logPrefix := fmt.Sprintf("[DRONE/%s]", droneID)
	addr := gatewayAddrs[idx]
	name := gatewayNames[idx]
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		log.Printf("%s Falha ao conectar no gateway %s (%s): %v", logPrefix, name, addr, err)
		return err
	}
	defer conn.Close()

	msg := Message{
		Type:        "DEVICE_REG",
		DroneID:     droneID,
		Content:     controlAddr,
		Status:      currentStatus(),
		MissionInfo: currentMissionInfo(),
	}

	if err := json.NewEncoder(conn).Encode(msg); err != nil {
		log.Printf("%s Falha ao registrar no gateway %s: %v", logPrefix, name, err)
		return err
	}

	stateMutex.Lock()
	currentGatewayIdx = idx
	currentGateway = addr
	currentGatewayName = name
	stateMutex.Unlock()

	log.Printf("%s Registrado com sucesso no gateway %s (%s). Status atual: %s", logPrefix, name, addr, currentStatus())
	if missionActive {
		log.Printf("%s MIGRACAO: drone migrou durante missão ativa para %s", logPrefix, name)
	}
	return nil
}

func heartbeatLoop(controlAddr string) {
	logPrefix := fmt.Sprintf("[DRONE/%s]", droneID)
	for {
		time.Sleep(3 * time.Second)
		log.Printf("%s [HEARTBEAT] Enviando heartbeat", logPrefix)
		if err := sendHeartbeat(); err != nil {
			log.Printf("%s [HEARTBEAT] Falha no heartbeat: %v", logPrefix, err)
			log.Printf("%s [MIGRACAO] Detectado gateway offline. Iniciando migração.", logPrefix)
			migrateGateway(controlAddr)
		}
	}
}

func sendHeartbeat() error {
	stateMutex.Lock()
	addr := currentGateway
	status := currentStatus()
	missionInfo := currentMissionInfo()
	stateMutex.Unlock()

	if addr == "" {
		return fmt.Errorf("sem gateway atual")
	}

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	msg := Message{
		Type:        "HEARTBEAT",
		DroneID:     droneID,
		Status:      status,
		MissionInfo: missionInfo,
	}

	if err := json.NewEncoder(conn).Encode(msg); err != nil {
		return err
	}

	log.Printf("[DRONE/%s] [HEARTBEAT] Heartbeat enviado ao gateway %s", droneID, addr)
	return nil
}

func migrateGateway(controlAddr string) {
	logPrefix := fmt.Sprintf("[DRONE/%s]", droneID)
	currentState := currentStatus()
	activeDuringMigration := missionActive
	fromName, fromAddr := currentGatewayName, currentGateway
	preferred := currentGatewayIdx
	order := gatewayOrder(preferred)

	for _, idx := range order {
		if idx == currentGatewayIdx {
			continue
		}
		if tryRegisterGateway(idx, controlAddr) == nil {
			log.Printf("%s [MIGRACAO] Migração bem-sucedida de %s (%s) para %s (%s)", logPrefix, fromName, fromAddr, gatewayNames[idx], gatewayAddrs[idx])
			if activeDuringMigration {
				log.Printf("%s [MIGRACAO] drone migrou durante missão ativa", logPrefix)
				log.Printf("%s [MIGRACAO] novo gateway informado de que o drone está %s", logPrefix, currentState)
			}
			return
		}
	}

	log.Printf("%s [MIGRACAO] Nenhum gateway alternativo disponível, aguardando 5s", logPrefix)
	time.Sleep(5 * time.Second)
}

func startCommandListener(controlAddr string) {
	logPrefix := fmt.Sprintf("[DRONE/%s]", droneID)
	listenerAddr := controlAddr
	listener, err := net.Listen("tcp", listenerAddr)
	if err != nil {
		log.Fatalf("%s Falha ao iniciar listener de comando: %v", logPrefix, err)
	}
	log.Printf("%s [COMANDO] Escutando comandos em %s", logPrefix, listenerAddr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go handleCommand(conn)
	}
}

func handleCommand(conn net.Conn) {
	defer conn.Close()
	var msg Message
	if err := json.NewDecoder(conn).Decode(&msg); err != nil {
		log.Printf("[DRONE/%s] [FALHA] Erro ao decodificar comando: %v", droneID, err)
		return
	}

	if msg.Type != "DISPATCH" {
		log.Printf("[DRONE/%s] [COMANDO] Comando desconhecido: %s", droneID, msg.Type)
		return
	}

	log.Printf("[DRONE/%s] [MISSAO] DISPATCH recebido", droneID)
	startMission()
}

func startMission() {
	stateMutex.Lock()
	if missionActive {
		log.Printf("[DRONE/%s] [MISSAO] Já em missão ativa, ignorando novo dispatch", droneID)
		stateMutex.Unlock()
		return
	}
	missionActive = true
	statusValue = "OCUPADO"
	duration := time.Duration(rand.Intn(11)+10) * time.Second
	missionEnd = time.Now().Add(duration)
	gatewayName := currentGatewayName
	stateMutex.Unlock()

	log.Printf("[DRONE/%s] [MISSAO] Iniciando missão de %s no gateway %s", droneID, duration, gatewayName)
	go func() {
		startTime := time.Now()
		time.Sleep(duration)
		elapsed := time.Since(startTime)

		stateMutex.Lock()
		missionActive = false
		statusValue = "DISPONIVEL"
		stateMutex.Unlock()

		log.Printf("[DRONE/%s] [MISSAO] Missão concluída após %s", droneID, elapsed)
		sendRelease()
	}()
}

func sendRelease() {
	logPrefix := fmt.Sprintf("[DRONE/%s]", droneID)
	for {
		stateMutex.Lock()
		addr := currentGateway
		stateMutex.Unlock()

		if addr == "" {
			log.Printf("%s [MISSAO] Sem gateway atual para enviar RELEASE, aguardando migração", logPrefix)
			time.Sleep(3 * time.Second)
			continue
		}

		log.Printf("%s [MISSAO] Enviando RELEASE ao gateway %s", logPrefix, addr)
		conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
		if err != nil {
			log.Printf("%s [MISSAO] Falha ao conectar para RELEASE: %v", logPrefix, err)
			migrateGateway(fmt.Sprintf("%s:%s", deviceHost, deviceControlPort))
			continue
		}

		msg := Message{Type: "RELEASE", DroneID: droneID}
		if err := json.NewEncoder(conn).Encode(&msg); err != nil {
			log.Printf("%s [MISSAO] Erro ao enviar RELEASE: %v", logPrefix, err)
			conn.Close()
			migrateGateway(fmt.Sprintf("%s:%s", deviceHost, deviceControlPort))
			continue
		}
		conn.Close()
		log.Printf("%s [MISSAO] RELEASE enviado com sucesso ao gateway %s", logPrefix, addr)
		return
	}
}

func currentStatus() string {
	stateMutex.Lock()
	defer stateMutex.Unlock()
	return statusValue
}

func currentMissionInfo() string {
	stateMutex.Lock()
	defer stateMutex.Unlock()
	if missionActive {
		return fmt.Sprintf("em missão até %s", missionEnd.Format(time.RFC3339))
	}
	return "sem missão"
}
