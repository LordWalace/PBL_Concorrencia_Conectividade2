package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Message struct {
	Type       string            `json:"type"`
	Priority   int               `json:"priority,omitempty"`
	Occurrence string            `json:"occurrence,omitempty"`
	Payload    map[string]string `json:"payload,omitempty"`
}

type DroneInfo struct {
	Status       string
	MissionState string
	Gateway      string
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

	clearScreen()

	for {
		fmt.Println("======================================")
		fmt.Println("  CLIENTE DO DESBLOQUEIO DO ESTREITO")
		fmt.Println("======================================")
		fmt.Println("\nMenu:")
		fmt.Println("1 - Injetar Alerta Manual")
		fmt.Println("2 - Ver Status do Estreito")
		fmt.Println("3 - Ver Log de Eventos")
		fmt.Println("0 - Sair")
		fmt.Print("Escolha uma opção (ou Enter para atualizar): ")

		choice := readChoice(reader)

		switch choice {
		case "1":
			clearScreen()
			sendManualAlert(reader, sectors, gateways)
			time.Sleep(2 * time.Second)
			clearScreen()

		case "2":
			clearScreen()
			printStatus(sectors, gateways)
			fmt.Println()

		case "3":
			clearScreen()
			viewEventLog(reader, sectors, gateways)
			fmt.Println()

		case "":
			clearMenuLines(11)
			continue

		case "0":
			clearScreen()
			fmt.Println("Encerrando cliente.")
			return

		default:
			clearMenuLines(11)
			continue
		}
	}
}

func sendManualAlert(reader *bufio.Reader, sectors []string, gateways map[string]string) {
	fmt.Println("--- INJETAR ALERTA MANUAL ---")
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

	fmt.Printf("\n[CLIENTE] Enviando alerta para %s com prioridade %d e ocorrência '%s'\n", setorEscolhido, priority, occurrence)
	sendWithFallback(msg, setorEscolhido, sectors, gateways)
}

func printStatus(sectors []string, gateways map[string]string) {
	fmt.Println("--- STATUS DO ESTREITO ---")

	var wg sync.WaitGroup
	var mu sync.Mutex
	globalDrones := make(map[string]DroneInfo)
	sectorResults := make([]string, len(sectors))

	for i, sector := range sectors {
		wg.Add(1)
		go func(idx int, setor string) {
			defer wg.Done()
			addr := gateways[setor]
			conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
			if err != nil {
				sectorResults[idx] = fmt.Sprintf("[Setor %s] OFFLINE", setor)
				return
			}
			defer conn.Close()

			if err := json.NewEncoder(conn).Encode(Message{Type: "STATUS_REQ"}); err != nil {
				sectorResults[idx] = fmt.Sprintf("[Setor %s] Erro ao solicitar status: %v", setor, err)
				return
			}

			var reply Message
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			if err := json.NewDecoder(conn).Decode(&reply); err != nil || reply.Type != "STATUS_REP" {
				sectorResults[idx] = fmt.Sprintf("[Setor %s] OFFLINE ou API de status indisponível", setor)
				return
			}

			queueSize := reply.Payload["queue_size"]
			sectorResults[idx] = fmt.Sprintf("[Setor %s] ONLINE | Fila pendente: %s", setor, queueSize)

			droneStates := make(map[string]DroneInfo)
			for key, value := range reply.Payload {
				if !strings.HasPrefix(key, "drone_") {
					continue
				}
				trimmed := strings.TrimPrefix(key, "drone_")

				var droneID, field string
				switch {
				case strings.HasSuffix(trimmed, "_status"):
					droneID = strings.TrimSuffix(trimmed, "_status")
					field = "status"
				case strings.HasSuffix(trimmed, "_mission_active"):
					droneID = strings.TrimSuffix(trimmed, "_mission_active")
					field = "mission_active"
				case strings.HasSuffix(trimmed, "_gateway_atual"):
					droneID = strings.TrimSuffix(trimmed, "_gateway_atual")
					field = "gateway_atual"
				default:
					continue
				}

				info := droneStates[droneID]
				switch field {
				case "status":
					info.Status = value
				case "mission_active":
					if value == "true" {
						info.MissionState = "Em missão"
					} else {
						info.MissionState = "Disponível"
					}
				case "gateway_atual":
					info.Gateway = value
				}
				droneStates[droneID] = info
			}

			mu.Lock()
			for id, info := range droneStates {
				name := cleanDroneName(id)
				existing := globalDrones[name]
				if info.Status != "" {
					existing.Status = info.Status
				}
				if info.MissionState != "" {
					existing.MissionState = info.MissionState
				}
				if info.Gateway != "" {
					existing.Gateway = info.Gateway
				}
				globalDrones[name] = existing
			}
			mu.Unlock()
		}(i, sector)
	}

	wg.Wait()

	for _, result := range sectorResults {
		fmt.Println(result)
	}

	fmt.Println("\n--- STATUS GLOBAL DA FROTA ---")
	if len(globalDrones) == 0 {
		fmt.Println("Nenhum drone conhecido no momento.")
		return
	}

	keys := make([]string, 0, len(globalDrones))
	for droneID := range globalDrones {
		keys = append(keys, droneID)
	}
	sort.Strings(keys)

	for _, droneID := range keys {
		info := globalDrones[droneID]
		status := info.Status
		if status == "" {
			status = "DESCONHECIDO"
		}
		mission := info.MissionState
		if mission == "" {
			mission = "Indefinido"
		}
		gateway := info.Gateway
		if gateway == "" {
			gateway = "-"
		}
		fmt.Printf("[%s] - Status: %s | Missão: %s | Gateway: %s\n", droneID, status, mission, gateway)
	}
}

func cleanDroneName(droneID string) string {
	if strings.HasPrefix(droneID, "drone_") {
		droneID = strings.TrimPrefix(droneID, "drone_")
	}
	return droneID
}

func viewEventLog(reader *bufio.Reader, sectors []string, gateways map[string]string) {
	fmt.Println("--- LOG DE EVENTOS ---")
	fmt.Print("Quantos eventos deseja ver por setor? ")
	eventCount := readNumber(reader, 1, 20)
	fmt.Println()

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
		if err := json.NewDecoder(conn).Decode(&reply); err != nil {
			fmt.Printf("[Setor %s] Falha ao receber eventos: %v\n", sector, err)
			conn.Close()
			continue
		}
		conn.Close()

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

func readChoice(reader *bufio.Reader) string {
	line, err := reader.ReadString('\n')
	if err != nil {
		time.Sleep(2 * time.Second)
		os.Exit(1)
		return ""
	}
	// Apenas retorna a string limpa. Se for inválido, o switch do main lida com isso apagando o menu de forma limpa.
	return strings.TrimSpace(line)
}

func readNumber(reader *bufio.Reader, min, max int) int {
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			time.Sleep(2 * time.Second)
			os.Exit(1)
			return 0
		}
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
				conn.Close()
				continue
			}
			conn.Close()
			fmt.Printf("[CLIENTE] Alerta salvo com sucesso no Setor %s (%s)\n", sector, target)
			return
		}

		fmt.Println("[CLIENTE] Toda a malha está offline. Tentando novamente em 5 segundos...")
		time.Sleep(5 * time.Second)
	}
}

func clearScreen() {
	fmt.Print("\033[H\033[2J\033[3J")
}

func clearMenuLines(linhas int) {
	// Nova função mágica: sobe o cursor 'N' linhas e apaga tudo abaixo dele
	fmt.Printf("\033[%dA\033[J", linhas)
}
