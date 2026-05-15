package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	Type       string            `json:"type"`
	Priority   int               `json:"priority,omitempty"`
	Occurrence string            `json:"occurrence,omitempty"`
	Payload    map[string]string `json:"payload,omitempty"`
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("[FATAL] Variável de ambiente obrigatória ausente: %s", key)
	}
	return v
}

func main() {
	clientPort := mustEnv("GATEWAY_TCP_CLIENT_PORT")
	gateways := map[string]string{
		"Norte": fmt.Sprintf("%s:%s", mustEnv("IP_NORTE"), clientPort),
		"Sul":   fmt.Sprintf("%s:%s", mustEnv("IP_SUL"), clientPort),
		"Leste": fmt.Sprintf("%s:%s", mustEnv("IP_LESTE"), clientPort),
		"Oeste": fmt.Sprintf("%s:%s", mustEnv("IP_OESTE"), clientPort),
	}

	reader := bufio.NewReader(os.Stdin)
	sectors := []string{"Norte", "Sul", "Leste", "Oeste"}

	fmt.Println("======================================")
	fmt.Println("  CLIENTE DO DESBLOQUEIO DO ESTREITO")
	fmt.Println("======================================")

	for {
		fmt.Println("\nMenu:")
		fmt.Println("1 - Injetar Alerta Manual")
		fmt.Println("2 - Ver Status do Estreito")
		fmt.Println("3 - Ver Log de Eventos")
		fmt.Println("0 - Sair")
		fmt.Print("Escolha uma opção: ")

		choice := readNumber(reader, 0, 3)

		switch choice {
		case 1:
			sendManualAlert(reader, sectors, gateways)
		case 2:
			printStatus(sectors, gateways)
		case 3:
			viewEventLog(reader, sectors, gateways)
		case 0:
			fmt.Println("Encerrando cliente.")
			return
		}
	}
}

func sendManualAlert(reader *bufio.Reader, sectors []string, gateways map[string]string) {
	fmt.Println("\n--- INJETAR ALERTA MANUAL ---")
	for i, setor := range sectors {
		fmt.Printf("%d - %s\n", i+1, setor)
	}
	fmt.Print("Selecione o setor (1-4): ")
	sectorIndex := readNumber(reader, 1, len(sectors)) - 1
	setorEscolhido := sectors[sectorIndex]

	fmt.Println("\nPrioridade:")
	fmt.Println("1 - Crítica")
	fmt.Println("2 - Alta")
	fmt.Println("3 - Média")
	fmt.Println("4 - Baixa")
	fmt.Print("Escolha a prioridade: ")
	priority := readNumber(reader, 1, 4)

	occurrences := []string{
		"embarcação civil à deriva",
		"objeto não identificado",
		"suspeita de bloqueio parcial de rota",
		"falha de sinalização",
		"congestionamento em corredor marítimo",
		"inspeção visual urgente",
		"replanejamento de tráfego por risco ambiental",
	}

	fmt.Println("\nTipo de ocorrência:")
	for i, desc := range occurrences {
		fmt.Printf("%d - %s\n", i+1, desc)
	}
	fmt.Print("Escolha o tipo de ocorrência: ")
	occurrenceIndex := readNumber(reader, 1, len(occurrences)) - 1
	occurrence := occurrences[occurrenceIndex]

	msg := Message{
		Type:       "ALERT",
		Priority:   priority,
		Occurrence: occurrence,
	}

	fmt.Printf("[CLIENTE] Enviando alerta para %s com prioridade %d e ocorrência '%s'\n", setorEscolhido, priority, occurrence)
	sendWithFallback(msg, setorEscolhido, sectors, gateways)
}

func printStatus(sectors []string, gateways map[string]string) {
	fmt.Println("\n--- STATUS DO ESTREITO ---")
	globalDrones := make(map[string]string)

	for _, sector := range sectors {
		addr := gateways[sector]
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			fmt.Printf("[Setor %s] OFFLINE\n", sector)
			continue
		}

		request := Message{Type: "STATUS_REQ"}
		if err := json.NewEncoder(conn).Encode(request); err != nil {
			fmt.Printf("[Setor %s] Erro ao enviar requisição de status: %v\n", sector, err)
			conn.Close()
			continue
		}

		var reply Message
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		err = json.NewDecoder(conn).Decode(&reply)
		conn.Close()

		if err != nil || reply.Type != "STATUS_REP" {
			fmt.Printf("[Setor %s] OFFLINE ou API de status indisponível\n", sector)
			continue
		}

		fmt.Printf("[Setor %s] ONLINE | Fila pendente: %s\n", sector, reply.Payload["queue_size"])
		for key, value := range reply.Payload {
			if strings.HasPrefix(key, "drone_") {
				droneID := strings.TrimPrefix(key, "drone_")
				fmt.Printf("   -> Drone %s: %s\n", droneID, value)
				if _, exists := globalDrones[droneID]; !exists {
					globalDrones[droneID] = value
				}
			}
		}
	}

	fmt.Println("\n--- STATUS GLOBAL DA FROTA ---")
	if len(globalDrones) == 0 {
		fmt.Println("Nenhum drone conhecido no momento.")
		return
	}
	for droneID, status := range globalDrones {
		fmt.Printf("   %s => %s\n", droneID, status)
	}
}

func viewEventLog(reader *bufio.Reader, sectors []string, gateways map[string]string) {
	fmt.Println("\n--- LOG DE EVENTOS ---")
	fmt.Print("Quantos eventos deseja ver por setor? ")
	eventCount := readNumber(reader, 1, 20)

	for _, sector := range sectors {
		addr := gateways[sector]
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			fmt.Printf("[Setor %s] OFFLINE ou indisponível para logs\n", sector)
			continue
		}

		request := Message{Type: "EVENTS_REQ", Payload: map[string]string{"count": strconv.Itoa(eventCount)}}
		if err := json.NewEncoder(conn).Encode(request); err != nil {
			fmt.Printf("[Setor %s] Falha ao solicitar eventos: %v\n", sector, err)
			conn.Close()
			continue
		}

		var reply Message
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		err = json.NewDecoder(conn).Decode(&reply)
		conn.Close()

		if err != nil {
			fmt.Printf("[Setor %s] Falha ao receber eventos: %v\n", sector, err)
			continue
		}

		if reply.Type != "EVENTS_REP" {
			fmt.Printf("[Setor %s] API de eventos não disponível\n", sector)
			continue
		}

		fmt.Printf("[Setor %s] Eventos recebidos:\n", sector)
		eventIndex := 1
		for eventIndex <= eventCount {
			key := fmt.Sprintf("event_%d", eventIndex)
			if value, ok := reply.Payload[key]; ok {
				fmt.Printf("   %d - %s\n", eventIndex, value)
			} else {
				break
			}
			eventIndex++
		}
		if eventIndex == 1 {
			fmt.Println("   Nenhum evento disponível.")
		}
	}
}

func readNumber(reader *bufio.Reader, min, max int) int {
	for {
		line, _ := reader.ReadString('\n')
		value, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || value < min || value > max {
			fmt.Printf("Entrada inválida. Digite um número entre %d e %d: ", min, max)
			continue
		}
		return value
	}
}

func sendWithFallback(msg Message, initialSector string, sectors []string, gateways map[string]string) {
	order := make([]string, 0, len(sectors))
	order = append(order, initialSector)
	for _, sector := range sectors {
		if sector != initialSector {
			order = append(order, sector)
		}
	}

	for {
		for _, sector := range order {
			target := gateways[sector]
			conn, err := net.DialTimeout("tcp", target, 3*time.Second)
			if err != nil {
				continue
			}
			if err := json.NewEncoder(conn).Encode(msg); err != nil {
				fmt.Printf("[CLIENTE] Falha ao enviar alerta para %s: %v\n", sector, err)
				conn.Close()
				continue
			}
			conn.Close()
			fmt.Printf("[CLIENTE] Alerta enviado com sucesso para %s (%s)\n", sector, target)
			return
		}

		fmt.Println("[CLIENTE] Nenhum gateway disponível. Tentando novamente em 5 segundos...")
		time.Sleep(5 * time.Second)
	}
}
