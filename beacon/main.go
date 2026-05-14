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
	Type       string `json:"type"`
	GatewayID  string `json:"gateway_id"`
	Priority   int    `json:"priority"`
	Lamport    int    `json:"lamport"`
	Occurrence string `json:"occurrence"`
}

func mustEnv(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		log.Fatalf("[FATAL] Variável de ambiente obrigatória ausente: %s", key)
	}
	return v
}

func main() {
	gatewayIP := mustEnv("GATEWAY_IP")
	clientPort := mustEnv("GATEWAY_TCP_CLIENT_PORT")
	setorID := mustEnv("SETOR_ID")
	targetAddr := fmt.Sprintf("%s:%s", gatewayIP, clientPort)

	log.Printf("[BEACON] Iniciando Sensores do Setor %s", setorID)

	go startSensor("Radar_Costeiro", mustEnv("RADAR_INTERVAL_MS"), targetAddr, setorID)
	go startSensor("Sensor_Naval", mustEnv("NAVAL_INTERVAL_MS"), targetAddr, setorID)
	go startSensor("Boia_Inteligente", mustEnv("BOIA_INTERVAL_MS"), targetAddr, setorID)
	go startSensor("Estacao_Comunicacao", mustEnv("ESTACAO_INTERVAL_MS"), targetAddr, setorID)

	select {} // Mantém rodando
}

func startSensor(name, intervalStr, targetAddr, setorID string) {
	intervalMs, _ := strconv.Atoi(intervalStr)
	for {
		time.Sleep(time.Duration(intervalMs) * time.Millisecond)

		// Gera ocorrência aleatória baseada no tipo de sensor
		priority := rand.Intn(4) + 1 // 1 a 4
		occurrence := generateOccurrence(name, priority)

		msg := Message{
			Type:       "ALERT",
			GatewayID:  setorID,
			Priority:   priority,
			Lamport:    0,
			Occurrence: occurrence,
		}

		conn, err := net.DialTimeout("tcp", targetAddr, 2*time.Second)
		if err == nil {
			json.NewEncoder(conn).Encode(msg)
			conn.Close()
			log.Printf("[%s] Alerta enviado: %s (Prio: %d)", name, occurrence, priority)
		} else {
			log.Printf("[%s] Falha ao enviar alerta para Gateway: %v", name, err)
		}
	}
}

func generateOccurrence(sensor string, priority int) string {
	switch priority {
	case 1:
		return fmt.Sprintf("[%s] CRITICA: Embarcação à deriva / Objeto não identificado", sensor)
	case 2:
		return fmt.Sprintf("[%s] ALTA: Suspeita de bloqueio / Falha de sinalização", sensor)
	case 3:
		return fmt.Sprintf("[%s] MEDIA: Congestionamento / Inspeção visual", sensor)
	default:
		return fmt.Sprintf("[%s] BAIXA: Risco ambiental moderado", sensor)
	}
}