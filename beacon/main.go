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

type BeaconAlert struct {
	BeaconID       string `json:"beacon_id"`
	SensorType     string `json:"tipo_sensor"`
	OccurrenceType string `json:"tipo_ocorrencia"`
	Priority       int    `json:"prioridade"`
	LamportClock   int    `json:"lamport_clock"`
	TimestampUnix  int64  `json:"timestamp_unix"`
}

var (
	lamportClock int
	clockMutex   sync.Mutex
)

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("[FATAL] Variável de ambiente obrigatória ausente: %s", key)
	}
	return v
}

func main() {
	gatewayIP := mustEnv("GATEWAY_IP")
	gatewayPort := mustEnv("GATEWAY_TCP_REG_PORT")
	setorID := mustEnv("SETOR_ID")

	beaconPrefix := fmt.Sprintf("[BEACON/%s]", setorID)
	log.Printf("%s Beacon iniciado", beaconPrefix)

	rand.Seed(time.Now().UnixNano())

	go runSensor("Radar_Costeiro", "RADAR_INTERVAL_MS", gatewayIP, gatewayPort, setorID)
	go runSensor("Sensor_Naval", "NAVAL_INTERVAL_MS", gatewayIP, gatewayPort, setorID)
	go runSensor("Boia_Inteligente", "BOIA_INTERVAL_MS", gatewayIP, gatewayPort, setorID)
	go runSensor("Estacao_Comunicacao", "ESTACAO_INTERVAL_MS", gatewayIP, gatewayPort, setorID)

	select {}
}

func runSensor(sensorName, intervalEnv, gatewayIP, gatewayPort, setorID string) {
	sensorPrefix := fmt.Sprintf("[SENSOR/%s]", sensorName)
	intervalMsStr := mustEnv(intervalEnv)
	intervalMs, err := strconv.Atoi(intervalMsStr)
	if err != nil || intervalMs <= 0 {
		log.Fatalf("%s Intervalo inválido em %s: %s", sensorPrefix, intervalEnv, intervalMsStr)
	}

	log.Printf("%s Goroutine iniciada com intervalo de %dms", sensorPrefix, intervalMs)

	for {
		time.Sleep(time.Duration(intervalMs) * time.Millisecond)

		priority := rand.Intn(4) + 1
		occurrence := generateOccurrence(priority)
		log.Printf("%s Ocorrência gerada: %s", sensorPrefix, occurrence)
		log.Printf("%s Prioridade sorteada: %d", sensorPrefix, priority)

		alert := BeaconAlert{
			BeaconID:       setorID,
			SensorType:     sensorName,
			OccurrenceType: occurrence,
			Priority:       priority,
			LamportClock:   nextLamport(),
			TimestampUnix:  time.Now().Unix(),
		}

		log.Printf("%s Enviando alerta com prioridade %d e Lamport %d", sensorPrefix, alert.Priority, alert.LamportClock)
		sendAlert(sensorPrefix, alert, gatewayIP, gatewayPort)
	}
}

func nextLamport() int {
	clockMutex.Lock()
	defer clockMutex.Unlock()
	lamportClock++
	return lamportClock
}

func generateOccurrence(priority int) string {
	switch priority {
	case 1:
		if rand.Intn(2) == 0 {
			return "embarcação civil à deriva"
		}
		return "objeto não identificado"
	case 2:
		if rand.Intn(2) == 0 {
			return "suspeita de bloqueio parcial de rota"
		}
		return "falha de sinalização"
	case 3:
		if rand.Intn(2) == 0 {
			return "congestionamento em corredor marítimo"
		}
		return "inspeção visual urgente"
	default:
		return "replanejamento de tráfego por risco ambiental"
	}
}

func sendAlert(sensorPrefix string, alert BeaconAlert, gatewayIP, gatewayPort string) {
	gatewayAddr := net.JoinHostPort(gatewayIP, gatewayPort)
	conn, err := net.DialTimeout("tcp", gatewayAddr, 3*time.Second)
	if err != nil {
		log.Printf("%s Falha ao enviar alerta: %v", sensorPrefix, err)
		return
	}
	defer conn.Close()

	message := map[string]interface{}{
		"type":       "ALERT",
		"occurrence": alert.OccurrenceType,
		"priority":   alert.Priority,
		"lamport":    alert.LamportClock,
		"timestamp":  alert.TimestampUnix,
		"payload": map[string]string{
			"beacon_id":   alert.BeaconID,
			"sensor_type": alert.SensorType,
		},
	}

	payload, err := json.Marshal(message)
	if err != nil {
		log.Printf("%s Falha ao serializar alerta: %v", sensorPrefix, err)
		return
	}

	_, err = conn.Write(payload)
	if err != nil {
		log.Printf("%s Falha ao enviar alerta: %v", sensorPrefix, err)
		return
	}

	log.Printf("%s Alerta enviado com sucesso ao gateway %s", sensorPrefix, gatewayAddr)
}
