package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	Type    string `json:"type"`
	DroneID string `json:"drone_id"`
	Content string `json:"content,omitempty"`
}

var (
	droneID     string
	gatewayAddr string
	failureRate float64
)

func mustEnv(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		log.Fatalf("[FATAL] Variável de ambiente obrigatória ausente: %s", key)
	}
	return v
}

func main() {
	droneID = mustEnv("DEVICE_ID")
	host := mustEnv("DEVICE_HOST")
	port := mustEnv("DEVICE_CONTROL_PORT")
	
	gatewayIP := mustEnv("GATEWAY_IP")
	regPort := mustEnv("GATEWAY_TCP_REG_PORT")
	gatewayAddr = fmt.Sprintf("%s:%s", gatewayIP, regPort)

	rate, _ := strconv.ParseFloat(mustEnv("FAILURE_RATE"), 64)
	failureRate = rate

	// 1. Inicia o listener de comandos do Gateway
	listenAddr := fmt.Sprintf("%s:%s", host, port)
	l, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("[FATAL] Falha ao escutar controle: %v", err)
	}
	
	// Determina IP real para informar ao Gateway (simplificação: usa IP do env)
	myIP := mustEnv("DEVICE_IP")
	myFullAddr := fmt.Sprintf("%s:%s", myIP, port)

	log.Printf("[DRONE/%s] Iniciado. Aguardando comandos em %s", droneID, myFullAddr)

	// 2. Registra no Gateway
	registerOnGateway(myFullAddr)

	// 3. Heartbeat loop
	go heartbeatLoop()

	// 4. Aceita comandos
	for {
		conn, err := l.Accept()
		if err != nil {
			continue
		}
		go handleCommand(conn)
	}
}

func registerOnGateway(myAddr string) {
	conn, err := net.Dial("tcp", gatewayAddr)
	if err == nil {
		msg := Message{Type: "DEVICE_REG", DroneID: droneID, Content: myAddr}
		json.NewEncoder(conn).Encode(msg)
		conn.Close()
		log.Printf("[DRONE] Registrado no Gateway em %s", gatewayAddr)
	}
}

func heartbeatLoop() {
	for {
		time.Sleep(3 * time.Second)
		conn, err := net.DialTimeout("tcp", gatewayAddr, 1*time.Second)
		if err == nil {
			json.NewEncoder(conn).Encode(Message{Type: "HEARTBEAT", DroneID: droneID})
			conn.Close()
		}
	}
}

func handleCommand(conn net.Conn) {
	defer conn.Close()
	var msg Message
	json.NewDecoder(conn).Decode(&msg)

	if msg.Type == "DISPATCH" {
		log.Printf("[DRONE/%s] Ordem de despacho recebida! Iniciando missão...", droneID)
		
		// Simula missão (10 a 30s)
		missionTime := time.Duration(rand.Intn(20)+10) * time.Second
		
		// Simula falha durante a missão
		if rand.Float64() < failureRate {
			time.Sleep(missionTime / 2)
			log.Fatalf("[DRONE/%s] FALHA CRÍTICA SIMULADA! Bateria esgotada/Abatido.", droneID)
			// O OS mata o container. O Gateway detecta via ausência de Heartbeat.
		}

		time.Sleep(missionTime)
		log.Printf("[DRONE/%s] Missão concluída. Notificando liberação.", droneID)
		
		// Avisa gateway que terminou
		connGW, err := net.Dial("tcp", gatewayAddr)
		if err == nil {
			json.NewEncoder(connGW).Encode(Message{Type: "RELEASE", DroneID: droneID})
			connGW.Close()
		}
	}
}