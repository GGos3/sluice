# sluice

**SSH 포트 하나만으로, 방화벽에 막힌 서버에서 인터넷을 쓸 수 있게 합니다.**

방화벽으로 인터넷이 차단된 서버에서도 외부로 나가는 SSH(TCP 22) 연결만 가능하면 `sluice`를 통해 자유롭게 인터넷을 사용할 수 있습니다. `sluice`는 프록시 서버에서 SSH 리버스 터널(-R)을 자동으로 생성하고, 클라이언트(agent)는 모든 HTTP, HTTPS, DNS 트래픽을 가로채어 이 터널로 전송합니다.

```text
┌──────────────────┐          ┌──────────────────┐          ┌──────────────┐
│  막힌 서버        │──────────│  프록시 서버      │──────────│  인터넷       │
│  (Client/Agent)  │  SSH     │  (Server)        │  HTTPS:  │              │
│                  │  터널    │                  │  E2E     │  github.com  │
│  curl, git,      │  (-R)    │  sluice server   │  TLS     │  pypi.org    │
│  apt, dnf ...    │ (암호화)  │                  │          │  google.com  │
└──────────────────┘          └──────────────────┘          └──────────────┘
       └────── 방화벽은 SSH 연결만 확인 가능, 통신 내용 열람 불가 ──────┘
```

## 주요 기능

- **SSH 리버스 터널링** — 별도의 포트 개방 없이 SSH(22) 포트 하나만으로 모든 트래픽 중계
- **투명 프록시 (Agent)** — 애플리케이션 설정 변경 없이 모든 트래픽(80, 443, 53)을 자동으로 가로채기
- **DNS-over-HTTPS (DoH)** — DNS 질의를 프록시 포트(18080)를 통해 암호화하여 전송, DNS 유출 차단
- **클라이언트 제외 규칙** — 사설 IP 대역(10.0.0.0/8 등)이나 특정 도메인은 프록시를 거치지 않도록 설정 가능
- **서버 화이트리스트** — 허용된 도메인만 접속할 수 있게 제한 (Default-Deny ACL)
- **구조화된 접근 로그** — 접속한 도메인, 상태 코드, 전송량 등을 JSON 형태로 기록
- **Docker 지원** — `server`와 `agent` 각각 최적화된 Docker 이미지 제공
- **단일 바이너리** — Go로 작성되어 의존성 없이 즉시 실행 가능

## 보안 및 아키텍처

`sluice`는 SSH의 강력한 암호화를 활용합니다. 방화벽 입장에서는 서버가 외부 프록시 서버와 SSH 연결을 유지하고 있는 것으로만 보이며, 그 안에서 어떤 사이트(github.com, google.com 등)에 접속하는지 알 수 없습니다.

| 구간 | 암호화 방식 | 설명 |
|------|-------------|------|
| 막힌 서버 ↔ 프록시 서버 | **SSH 터널** | 방화벽이 트래픽 내용 및 접속 대상을 볼 수 없음 |
| 막힌 서버 ↔ 목적지 서버 | **TLS (E2E)** | 프록시 서버도 HTTPS 통신 내용을 볼 수 없음 |

> **참고:** `agent` 모드는 Linux 전용입니다. TUN 디바이스와 netstack을 사용하여 트래픽을 가로챕니다.

## 빠른 시작

### 1. 프록시 서버 시작 (내 로컬 PC 또는 외부 서버)

프록시 서버에서 아래 명령을 실행하면 프록시 서비스를 시작하고, 동시에 막힌 서버에 SSH로 접속하여 리버스 터널을 생성합니다.

```bash
# user@remote-host는 방화벽에 막힌 서버의 SSH 주소입니다.
./sluice server --tunnel user@remote-host
```

### 2. 에이전트 시작 (막힌 서버)

막힌 서버에서 루트 권한으로 에이전트를 실행합니다. 이제 이 서버의 모든 인터넷 트래픽은 자동으로 프록시를 경유합니다.

```bash
sudo ./sluice agent
```

### 3. 확인

이제 아무 명령어나 실행해 보세요.

```bash
curl https://github.com
git clone https://github.com/example/repo.git
```

## Docker 사용법

### 서버 (Server)

```bash
docker run -d --name sluice-server \
  -v ~/.ssh:/root/.ssh:ro \
  ghcr.io/ggos3/sluice-server \
  server --tunnel user@remote-host
```

### 에이전트 (Agent)

에이전트는 네트워크 트래픽을 가로채기 위해 `--net=host`와 `NET_ADMIN` 권한이 필요합니다.

```bash
docker run -d --name sluice-agent \
  --net=host \
  --cap-add=NET_ADMIN \
  ghcr.io/ggos3/sluice-agent
```

## 상세 설정

### 서버 설정 (`configs/config.yaml`)

서버는 화이트리스트를 통해 접속 가능한 도메인을 제한할 수 있습니다.

```yaml
server:
  port: 18080
whitelist:
  enabled: true
  domains:
    - "github.com"
    - "*.github.com"
    - "pypi.org"
auth:
  enabled: false
```

### 에이전트 제외 규칙 (`--no-proxy`)

내부 망 통신 등 프록시를 거치지 않아야 할 대상은 `--no-proxy` 플래그로 지정합니다. 도메인(와일드카드 지원)과 IP 대역(CIDR)을 혼합해서 쓸 수 있습니다.

```bash
# 기본값으로 127.0.0.0/8, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16은 제외됩니다.
sudo ./sluice agent --no-proxy "*.internal.corp,172.20.0.0/16"
```

## 수동 SSH 터널링 (선택 사항)

`sluice server --tunnel`을 사용하는 대신 직접 SSH 터널을 뚫고 싶다면 다음과 같이 합니다:

1. 프록시 서버 실행: `sluice server --port 18080` (직접 리슨)
2. 터널 생성: `ssh -R 18080:localhost:18080 user@remote-host -N`
3. 에이전트 실행: `sudo sluice agent --port 18080`

## 실행 모드 요약

| 모드 | 용도 | 권한 |
|------|------|------|
| `server` | 프록시 서비스 및 SSH 터널 관리 | 일반 유저 |
| `agent` | 시스템 전체 트래픽 투명 가로채기 | **Root (Linux)** |
| `run` | 특정 명령어만 프록시 경유 (환경변수 방식) | 일반 유저 |

### Run 모드 사용법

시스템 전체를 가로채지 않고 특정 명령어만 프록시를 쓰게 하고 싶을 때 유용합니다. (Agent 실행 불필요, SSH 터널은 필요)

```bash
# 프록시 서버가 localhost:18080에 터널링되어 있다고 가정
./sluice run -- curl https://example.com
```

## 접근 로그 (Access Log)

프록시 서버는 모든 요청을 JSON 구조화 로그로 남깁니다.

```json
{
  "time": "2026-03-10T10:15:32Z",
  "level": "INFO",
  "msg": "access",
  "proxy": {
    "source_ip": "127.0.0.1",
    "method": "CONNECT",
    "domain": "github.com:443",
    "status": 200,
    "bytes_in": 1240,
    "bytes_out": 5620,
    "duration_ms": 150,
    "allowed": true,
    "reason": "ok"
  }
}
```

## 프로젝트 구조

- `cmd/sluice/`: CLI 엔트리포인트 (server, agent, run)
- `internal/proxy/`: HTTP/HTTPS 프록시 핸들러 및 터널링
- `internal/tunnel/`: SSH 리버스 터널 매니저
- `internal/dns/`: DoH(DNS-over-HTTPS) 서버 구현
- `internal/gateway/`: TUN/netstack 기반 트래픽 인터셉터 (Agent 코어)
- `internal/rules/`: 클라이언트 제외 규칙 엔진
- `internal/acl/`: 서버 측 도메인 화이트리스트

## 라이선스

MIT
