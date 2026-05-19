package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
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

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("[FATAL] Variável de ambiente obrigatória ausente: %s", key)
	}
	return v
}

func main() {
	localGatewayIP := mustEnv("GATEWAY_IP")
	gatewayPort := mustEnv("GATEWAY_TCP_REG_PORT")
	setorID := mustEnv("SETOR_ID")

	gatewayAddrs := map[string]string{
		"Norte": fmt.Sprintf("%s:%s", mustEnv("IP_NORTE"), gatewayPort),
		"Sul":   fmt.Sprintf("%s:%s", mustEnv("IP_SUL"), gatewayPort),
		"Leste": fmt.Sprintf("%s:%s", mustEnv("IP_LESTE"), gatewayPort),
		"Oeste": fmt.Sprintf("%s:%s", mustEnv("IP_OESTE"), gatewayPort),
	}

	localGatewayAddr := fmt.Sprintf("%s:%s", localGatewayIP, gatewayPort)
	logPrefix := fmt.Sprintf("[BEACON/%s]", setorID)
	log.Printf("%s Beacon iniciado", logPrefix)

	rand.Seed(time.Now().UnixNano())

	go runSensor("Radar_Costeiro", "RADAR_INTERVAL_MS", setorID, localGatewayAddr, gatewayAddrs)
	go runSensor("Sensor_Naval", "NAVAL_INTERVAL_MS", setorID, localGatewayAddr, gatewayAddrs)
	go runSensor("Boia_Inteligente", "BOIA_INTERVAL_MS", setorID, localGatewayAddr, gatewayAddrs)
	go runSensor("Estacao_Comunicacao", "ESTACAO_INTERVAL_MS", setorID, localGatewayAddr, gatewayAddrs)

	select {}
}

func runSensor(sensorName, intervalEnv, setorID, localGatewayAddr string, gatewayAddrs map[string]string) {
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
			LamportClock:   int(time.Now().UnixNano()),
			TimestampUnix:  time.Now().Unix(),
		}

		log.Printf("%s Enviando alerta com prioridade %d", sensorPrefix, alert.Priority)
		sendAlert(sensorPrefix, alert, localGatewayAddr, gatewayAddrs)
	}
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

func sendAlert(sensorPrefix string, alert BeaconAlert, localGatewayAddr string, gatewayAddrs map[string]string) {
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

	for {
		if trySendToGateway(sensorPrefix, localGatewayAddr, payload) {
			return
		}

		log.Printf("%s Gateway local indisponível. Tentando encaminhar aos vizinhos.", sensorPrefix)
		for region, addr := range gatewayAddrs {
			if addr == localGatewayAddr {
				continue
			}
			if trySendToGateway(sensorPrefix, addr, payload) {
				log.Printf("%s Alerta encaminhado com sucesso para gateway %s (%s)", sensorPrefix, region, addr)
				return
			}
		}

		log.Printf("%s Nenhum gateway respondeu. Repetindo envio em 5 segundos.", sensorPrefix)
		time.Sleep(5 * time.Second)
	}
}

func trySendToGateway(sensorPrefix, addr string, payload []byte) bool {
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		log.Printf("%s Falha ao conectar no gateway %s: %v", sensorPrefix, addr, err)
		return false
	}
	defer conn.Close()

	if _, err := conn.Write(payload); err != nil {
		log.Printf("%s Falha ao enviar alerta: %v", sensorPrefix, err)
		return false
	}

	log.Printf("%s Alerta enviado com sucesso ao gateway %s", sensorPrefix, addr)
	return true
}
