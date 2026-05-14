# 🚀 Sistema Distribuido de Drones (Pub/Sub + Ricart-Agrawala)

Um sistema distribuido para coordenar drones autonomos entre quatro setores (Norte, Sul, Leste, Oeste). Cada componente roda em um PC fisico diferente e usa IPs reais da LAN. Nao ha ponto unico de falha.

## 📋 Sumario

- Visao Geral
- Arquitetura
- Pre-requisitos
- Configuracao
- Quick Start
- Comandos Docker
- Como Usar
- Estrutura do Projeto
- Troubleshooting

---

## 🎯 Visao Geral

Componentes:

- **Gateway** — Recebe ocorrencias, coordena exclusao mutua P2P e despacha drones locais.
- **Beacon** — Publica telemetria via UDP no gateway local.
- **Device** — Escuta comandos do gateway e simula a missao.
- **Client** — Injeta ocorrencias manuais via TCP (com ACK do gateway).

Caracteristicas principais:

- ✅ Exclusao mutua distribuida (Ricart-Agrawala + Lamport)
- ✅ Sem leader e sem ponto unico de falha
- ✅ Telemetria UDP (beacons) + comandos TCP (client/device)
- ✅ IPs reais da LAN e listeners em 0.0.0.0

---

## 🏗️ Arquitetura

```
┌──────────────────────────────────────────────────────────────┐
│                          GATEWAY                             │
│  UDP: GATEWAY_UDP_PORT    (beacon -> gateway)                │
│  TCP: GATEWAY_TCP_CLIENT_PORT (client -> gateway)            │
│  TCP: GATEWAY_TCP_REG_PORT    (gateway <-> gateway, RA)      │
└──────────────────────────────────────────────────────────────┘
	     ▲                         ▲                   ▲
	     │ UDP                     │ TCP               │ TCP
	     │                         │                   │
   ┌─────┴─────┐              ┌────┴────┐         ┌────┴────┐
   │  BEACON   │              │ CLIENT  │         │ DEVICE  │
   │ Telemetria│              │ Manual  │         │ Drone   │
   └───────────┘              └─────────┘         └─────────┘
```

### Componentes

| Componente | Porta | Protocolo | Funcao |
|-----------|-------|-----------|--------|
| Gateway | `GATEWAY_UDP_PORT` | UDP | Recepcao de telemetria (beacons) |
| Gateway | `GATEWAY_TCP_CLIENT_PORT` | TCP | Ocorrencias manuais (client) |
| Gateway | `GATEWAY_TCP_REG_PORT` | TCP | Exclusao mutua P2P (RA) |
| Device | `DEVICE_CONTROL_PORT` | TCP | Execucao de missao |

---

## 📦 Pre-requisitos

- Docker 20.10+
- Docker Compose 2.0+
- Go 1.22+ (apenas para build local)

---

## ⚙️ Configuracao

### Variaveis de Ambiente (.env)

Copie o exemplo e edite os IPs reais:

```bash
copy .env.example .env
```

Campos obrigatorios:

- `IP_NORTE`, `IP_SUL`, `IP_LESTE`, `IP_OESTE`
- `GATEWAY_ID`, `GATEWAY_IP`, `GATEWAY_HOST=0.0.0.0`
- `GATEWAY_TCP_CLIENT_PORT`
- `DEVICE_ID`, `DEVICE_IP`, `DEVICE_HOST=0.0.0.0`, `DEVICE_CONTROL_PORT`
- `BEACON_ID`

Restricoes:
- `GATEWAY_TCP_CLIENT_PORT` deve ser o mesmo em todos os gateways e no client remoto.

- Nao use `localhost` ou `127.0.0.1`.
- Conexoes usam IPs reais da LAN.
- Servidores escutam em `0.0.0.0`.

---

## 🚀 Quick Start

```bash
docker compose build
```

Em cada PC, rode apenas o servico correspondente:

```bash
docker compose up gateway
docker compose up device
docker compose up beacon
docker compose up client
```

---

## 🐳 Comandos Docker

```bash
docker compose build
docker compose up gateway
docker compose up device
docker compose up beacon
docker compose up client
docker compose down
```

---

## 💻 Como Usar

- Inicie gateways primeiro em cada setor.
- Suba devices antes de enviar ocorrencias.
- Use o client para ocorrencias criticas e validar ACK.

---

## 📂 Estrutura do Projeto

```text
PBL_Redes-Sensores2/
├── README.md
├── README_PRIVADO.md
├── docker-compose.yml
├── .env
├── .env.example
├── gateway/
├── device/
├── beacon/
└── client/
```

---

## 🔍 Troubleshooting

### Gateway desconectado

```bash
docker compose ps
docker compose logs gateway
```

### Porta em uso

```bash
netstat -ano | findstr :8080
taskkill /PID <PID> /F
```

---

## ✅ Checklist

- [ ] IPs reais configurados no `.env`
- [ ] `GATEWAY_HOST=0.0.0.0` e `DEVICE_HOST=0.0.0.0`
- [ ] Portas liberadas no firewall
- [ ] Gateways rodando antes de devices/beacons/clients
