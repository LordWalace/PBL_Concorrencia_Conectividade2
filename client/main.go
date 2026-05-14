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
	v := strings.TrimSpace(os.Getenv(key))
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

	for {
		fmt.Println("\n=======================================")
		fmt.Println("  PAINEL DO ESTREITO DE HORMUZ (PBL 2) ")
		fmt.Println("=======================================")
		fmt.Println("1 - Injetar Alerta Manual")
		fmt.Println("2 - Ver Status do Estreito")
		fmt.Println("0 - Sair")
		fmt.Print("Opção: ")

		opt, _ := reader.ReadString('\n')
		opt = strings.TrimSpace(opt)

		switch opt {
		case "1":
			sendManualAlert(reader, gateways)
		case "2":
			printStatus(gateways)
		case "0":
			os.Exit(0)
		default:
			fmt.Println("Opção inválida.")
		}
	}
}

func sendManualAlert(reader *bufio.Reader, gateways map[string]string) {
	fmt.Print("Selecione o Setor (Norte, Sul, Leste, Oeste): ")
	setor, _ := reader.ReadString('\n')
	setor = strings.TrimSpace(setor)

	target, exists := gateways[setor]
	if !exists {
		fmt.Println("Setor inválido.")
		return
	}

	fmt.Println("Prioridade: 1(Crítica), 2(Alta), 3(Média), 4(Baixa)")
	fmt.Print("Prioridade: ")
	pStr, _ := reader.ReadString('\n')
	p, _ := strconv.Atoi(strings.TrimSpace(pStr))

	fmt.Print("Descrição: ")
	desc, _ := reader.ReadString('\n')

	msg := Message{
		Type:       "ALERT",
		Priority:   p,
		Occurrence: "[MANUAL] " + strings.TrimSpace(desc),
	}

	conn, err := net.Dial("tcp", target)
	if err != nil {
		fmt.Println("[ERRO] Falha ao conectar no Gateway do setor.")
		return
	}
	defer conn.Close()
	json.NewEncoder(conn).Encode(msg)
	fmt.Println("[OK] Alerta enviado com sucesso.")
}

func printStatus(gateways map[string]string) {
	fmt.Println("\n--- STATUS GLOBAL DOS SETORES ---")
	for name, addr := range gateways {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			fmt.Printf("[Setor %s] OFFLINE\n", name)
			continue
		}
		
		msg := Message{Type: "STATUS_REQ"}
		json.NewEncoder(conn).Encode(msg)
		
		var rep Message
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		err = json.NewDecoder(conn).Decode(&rep)
		conn.Close()

		if err == nil && rep.Type == "STATUS_REP" {
			fmt.Printf("[Setor %s] ONLINE | Fila: %s\n", name, rep.Payload["queue_size"])
			for k, v := range rep.Payload {
				if strings.HasPrefix(k, "drone_") {
					fmt.Printf("   -> %s: %s\n", k, v)
				}
			}
		} else {
			fmt.Printf("[Setor %s] OFFLINE (Timeout)\n", name)
		}
	}
}