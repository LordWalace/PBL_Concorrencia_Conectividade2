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
	SensorType string `json:"sensor_type"`
}

func mustEnv(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		log.Fatalf("[FATAL] Variável ausente: %s", key)
	}
	return v
}

func main() {
	gatewayIP := mustEnv("GATEWAY_IP")
	regPort := mustEnv("GATEWAY_TCP_REG_PORT")
	setorID := mustEnv("SETOR_ID")
	targetAddr := fmt.Sprintf("%s:%s", gatewayIP, regPort)

	log.Printf("[BEACON] Agregador de Sensores do Setor %s iniciado.", setorID)

	go startSensor("Radar_Costeiro", mustEnv("RADAR_INTERVAL_MS"), targetAddr, setorID)
	go startSensor("Sensor_Naval", mustEnv("NAVAL_INTERVAL_MS"), targetAddr, setorID)
	go startSensor("Boia_Inteligente", mustEnv("BOIA_INTERVAL_MS"), targetAddr, setorID)
	go startSensor("Estacao_Comunicacao", mustEnv("ESTACAO_INTERVAL_MS"), targetAddr, setorID)

	select {}
}

func startSensor(sensorName, intervalStr, targetAddr, setorID string) {
	intervalMs, _ := strconv.Atoi(intervalStr)
	for {
		time.Sleep(time.Duration(intervalMs) * time.Millisecond)

		priority := rand.Intn(4) + 1
		occurrence := generateOccurrence(sensorName, priority)

		msg := Message{
			Type:       "ALERT",
			GatewayID:  setorID,
			Priority:   priority,
			Occurrence: occurrence,
			SensorType: sensorName,
		}

		conn, err := net.DialTimeout("tcp", targetAddr, 2*time.Second)
		if err == nil {
			json.NewEncoder(conn).Encode(msg)
			conn.Close()
		}
	}
}

func generateOccurrence(sensor string, priority int) string {
	switch priority {
	case 1:
		return "CRITICA: Embarcação à deriva / Objeto não identificado"
	case 2:
		return "ALTA: Suspeita de bloqueio / Falha de sinalização"
	case 3:
		return "MEDIA: Congestionamento / Inspeção visual urgente"
	default:
		return "BAIXA: Replanejamento por risco ambiental"
	}
}
