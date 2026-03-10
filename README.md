# sluice

**SSH 연결만 가능하면 인터넷을 사용할 수 있습니다.**

방화벽으로 인터넷이 차단된 서버에서 SSH 터널을 통해 프록시 서버에 연결하면 외부 인터넷에 접근할 수 있습니다. HTTPS 트래픽은 TLS로 종단 간 암호화(E2E)되어 프록시 서버를 포함한 어떤 중간 노드도 통신 내용을 열람할 수 없으며, SSH 터널을 사용하면 방화벽에서 접속 대상조차 확인할 수 없습니다.

```
┌──────────────────┐          ┌──────────────────┐          ┌──────────────┐
│  차단된 서버      │──────────│  프록시 서버      │──────────│  인터넷       │
│                  │  SSH     │  (sluice server) │  HTTPS:  │              │
│  curl, git,      │  터널    │  :8080           │  E2E     │  github.com  │
│  npm, pip ...    │ (암호화)  │                  │  TLS     │  pypi.org    │
└──────────────────┘          └──────────────────┘          └──────────────┘
       └────── 방화벽은 SSH 연결만 확인 가능, 통신 내용 열람 불가 ──────┘
```

## 주요 기능

- **SSH 터널만으로 인터넷 접근** — SSH(TCP 22)만 열려 있으면 외부 인터넷 사용 가능, 추가 포트 개방 불필요
- **종단 간 암호화 (E2E)** — HTTPS 트래픽은 TLS로 E2E 암호화되어 프록시 서버도 통신 내용 열람 불가
- **HTTP / HTTPS 프록싱** — 일반 HTTP 요청 포워딩 및 CONNECT 메서드를 통한 HTTPS 터널링
- **도메인 화이트리스트** — `*.github.com` 같은 와일드카드 패턴 지원, 미등록 도메인은 자동 차단 (default-deny)
- **접근 로그** — JSON 구조화 로그로 소스 IP, 도메인, 상태 코드, 전송 바이트, 응답 시간 기록
- **선택적 인증** — Proxy-Authorization 기반 Basic Auth 지원
- **게이트웨이 모드** — 순수 Go TUN/netstack 기반 투명 프록시, 외부 바이너리(iptables, redsocks) 의존 없음 (개발 중)
- **Docker 지원** — server / run / gateway 모드 컨테이너로 간편 배포
- **단일 바이너리** — Go로 작성되어 의존성 없이 배포 가능, 크로스 컴파일 지원

## 보안

### 종단 간 암호화 (E2E Encryption)

sluice를 통한 HTTPS 트래픽은 이중으로 암호화됩니다:

| 구간 | 암호화 방식 | 설명 |
|------|-------------|------|
| 차단된 서버 ↔ 프록시 서버 | **SSH 터널** | 방화벽이 트래픽 내용 및 접속 대상을 볼 수 없음 |
| 차단된 서버 ↔ 목적지 서버 | **TLS (E2E)** | 프록시 서버도 HTTPS 통신 내용을 볼 수 없음 |

프록시 서버는 HTTPS 요청 시 CONNECT 터널을 생성하고 암호화된 바이트를 그대로 중계합니다. 통신 내용을 복호화하거나 열람하지 않습니다.

> **로드맵:** 순수 Go 구현의 내장 E2E 암호화를 통해 SSH 터널 없이도 게이트웨이-서버 간 암호화 통신을 지원할 예정입니다.

### 네트워크 요구 사항

sluice는 최소한의 네트워크 연결만 필요합니다:

| 구간 | 프로토콜 | 포트 | 용도 |
|------|----------|------|------|
| 차단된 서버 → 프록시 서버 | TCP (SSH) | **22** | SSH 터널 (권장) |
| 차단된 서버 → 프록시 서버 | TCP | **8080** | 직접 연결 (SSH 불필요 시) |
| 프록시 서버 → 인터넷 | TCP | 80, 443 | HTTP/HTTPS 대상 서버 |

**SSH 터널을 사용하면 차단된 서버에서 TCP 22번 포트만 열려 있으면 됩니다.** 프록시 포트(8080)는 SSH 터널을 통해 localhost로 접근하므로 별도로 개방할 필요가 없습니다.

## 빠른 시작

sluice는 세 가지 모드로 동작합니다:

| 모드 | 용도 | 필요한 것 |
|------|------|-----------|
| **server** | 프록시 서버 실행 | 없음 (설정 자동 생성) |
| **run** | 프록시 경유로 명령어 실행 | 서버 주소 (`SLUICE_PROXY_HOST`) |
| **gateway** | 호스트 전체 트래픽 투명 프록시 | 서버 주소 + 루트 권한 |

### 서버 모드

프록시 서버를 실행합니다. HTTP 요청은 직접 포워딩하고, HTTPS 요청은 CONNECT 터널링으로 처리합니다. 설정 파일이 없으면 기본값(화이트리스트 비활성, 인증 비활성)으로 자동 생성됩니다.

#### Docker

```bash
docker run -d --name sluice -e SLUICE_MODE=server -p 8080:8080 ghcr.io/ggos3/sluice
```

커스텀 설정 파일을 사용하려면 `-v` 옵션으로 마운트합니다:

```bash
docker run -d --name sluice -e SLUICE_MODE=server -p 8080:8080 -v ./config.yaml:/etc/sluice/config.yaml:ro ghcr.io/ggos3/sluice
```

정지 및 삭제:

```bash
docker stop sluice && docker rm sluice
```

#### Docker Compose

```yaml
services:
  sluice-server:
    image: ghcr.io/ggos3/sluice
    environment:
      SLUICE_MODE: server
    ports:
      - "8080:8080"
```

```bash
docker compose up -d sluice-server    # 실행
docker compose down                   # 정지
```

### SSH 터널로 프록시 사용

차단된 서버에서 프록시 서버로 직접 연결이 불가능한 경우, SSH 터널을 통해 접근합니다. SSH 연결만 가능하면 모든 인터넷 트래픽을 프록시로 경유할 수 있습니다.

```bash
# 1. SSH 터널 생성 (로컬 8080 → 프록시 서버 8080)
ssh -L 8080:localhost:8080 user@proxy-server -N &

# 2. 프록시 환경 변수 설정
export HTTP_PROXY=http://localhost:8080
export HTTPS_PROXY=http://localhost:8080

# 3. 인터넷 사용 — 모든 트래픽이 SSH 터널을 통해 암호화
curl https://github.com
git clone https://github.com/user/repo
pip install requests
npm install
```

`scripts/setup-client.sh`를 사용하면 SSH 터널과 프록시 환경 변수를 자동으로 설정할 수 있습니다:

```bash
sudo ./scripts/setup-client.sh \
  --proxy-host proxy-server \
  --ssh-tunnel \
  --ssh-user myuser \
  --install
```

### Run 모드

프록시 서버를 경유하여 명령어를 실행합니다. 컨테이너는 명령 완료 후 자동으로 제거됩니다.

```bash
docker run --rm -e SLUICE_PROXY_HOST=192.168.1.100 ghcr.io/ggos3/sluice curl https://example.com

docker run --rm -e SLUICE_PROXY_HOST=192.168.1.100 ghcr.io/ggos3/sluice wget https://example.com/file.tar.gz

docker run --rm -e SLUICE_PROXY_HOST=192.168.1.100 ghcr.io/ggos3/sluice git clone https://github.com/user/repo

docker run --rm -e SLUICE_PROXY_HOST=192.168.1.100 ghcr.io/ggos3/sluice npm install
```

인터랙티브 셸 (프록시 환경변수가 자동 설정된 셸):

```bash
docker run -it --rm -e SLUICE_PROXY_HOST=192.168.1.100 ghcr.io/ggos3/sluice
```

프록시 인증이 필요한 경우:

```bash
docker run --rm -e SLUICE_PROXY_HOST=192.168.1.100 -e SLUICE_PROXY_USER=user1 -e SLUICE_PROXY_PASS=secret ghcr.io/ggos3/sluice curl https://example.com
```

### 게이트웨이 모드

호스트의 모든 아웃바운드 HTTP/HTTPS 트래픽을 투명하게 가로채어 프록시 서버로 라우팅합니다. 애플리케이션에 프록시 설정을 하지 않아도 모든 트래픽이 자동으로 프록시를 경유합니다.

> **참고:** 게이트웨이 모드는 순수 Go TUN/netstack 기반으로 재구현 중입니다. iptables, redsocks 등 외부 바이너리 의존이 제거되며, 향후 내장 E2E 암호화를 지원할 예정입니다.

**주의:** `--net=host`와 `NET_ADMIN`, `NET_RAW` 권한이 필요합니다.

#### Docker

```bash
docker run -d --name sluice-gw --net=host --cap-add=NET_ADMIN --cap-add=NET_RAW -e SLUICE_MODE=gateway -e SLUICE_PROXY_HOST=192.168.1.100 ghcr.io/ggos3/sluice
```

특정 도메인만 프록시하려면 `SLUICE_PROXY_DOMAINS`를 지정합니다:

```bash
docker run -d --name sluice-gw --net=host --cap-add=NET_ADMIN --cap-add=NET_RAW -e SLUICE_MODE=gateway -e SLUICE_PROXY_HOST=192.168.1.100 -e SLUICE_PROXY_DOMAINS="github.com,*.github.com,pypi.org" ghcr.io/ggos3/sluice
```

정지:

```bash
docker stop sluice-gw && docker rm sluice-gw
```

#### Docker Compose

```yaml
services:
  sluice-gateway:
    image: ghcr.io/ggos3/sluice
    environment:
      SLUICE_MODE: gateway
      SLUICE_PROXY_HOST: 192.168.1.100
    network_mode: host
    cap_add:
      - NET_ADMIN
      - NET_RAW
```

```bash
docker compose up -d sluice-gateway    # 실행
docker compose down                    # 정지
```

### 환경 변수

| 변수 | 설명 | 기본값 |
|---|---|---|
| `SLUICE_MODE` | `server`, `run`, 또는 `gateway` | `run` |
| `SLUICE_PROXY_HOST` | 프록시 서버 주소 (run/gateway 모드 필수) | - |
| `SLUICE_PROXY_PORT` | 프록시 서버 포트 | `8080` |
| `SLUICE_PROXY_USER` | 프록시 인증 사용자 | - |
| `SLUICE_PROXY_PASS` | 프록시 인증 비밀번호 | - |
| `SLUICE_PROXY_DOMAINS` | 프록시할 도메인 목록 (콤마 구분, 게이트웨이 모드) | - |
| `SLUICE_REDIRECT_PORTS` | 리다이렉트 포트 모드 (게이트웨이 모드) | `http` |
| `SLUICE_NO_PROXY` | 프록시 제외 대상 | `localhost,127.0.0.1,...` |
| `SLUICE_CONFIG` | 서버 모드 설정 파일 경로 | `/etc/sluice/config.yaml` |

## 바이너리 빌드

Go 1.24 이상이 필요합니다.

```bash
make build
```

크로스 컴파일 (linux/darwin, amd64/arm64):

```bash
make cross-build
```

## 바이너리 실행

```bash
./bin/sluice -config configs/config.yaml
```

## 설정

설정 파일 없이 서버를 시작하면 기본값으로 설정 파일이 자동 생성됩니다 (Docker: `/etc/sluice/config.yaml`, 바이너리: `configs/config.yaml`). 도메인 화이트리스트나 인증이 필요한 경우 `configs/config.yaml`을 참고하여 커스텀 설정 파일을 작성합니다.

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  read_timeout: 30
  write_timeout: 30
  idle_timeout: 120

whitelist:
  enabled: true
  domains:
    - "github.com"
    - "*.github.com"
    - "*.githubusercontent.com"
    - "go.dev"
    - "*.golang.org"
    - "proxy.golang.org"
    - "registry.npmjs.org"
    - "pypi.org"
    - "*.pypi.org"

logging:
  level: "info"        # debug, info, warn, error
  format: "json"       # json, text
  access_log: "stdout" # stdout, stderr, 또는 파일 경로

auth:
  enabled: false
  credentials:
    - username: "user1"
      password: "changeme"
```

### 화이트리스트 규칙

| 패턴 | 매칭 대상 | 비매칭 대상 |
|---|---|---|
| `github.com` | `github.com` | `api.github.com` |
| `*.github.com` | `api.github.com`, `raw.github.com` | `github.com` (apex 미매칭) |

apex 도메인과 서브도메인 모두 허용하려면 두 항목을 함께 등록합니다:

```yaml
domains:
  - "github.com"
  - "*.github.com"
```

## 클라이언트 설정

Docker 대신 직접 설정할 경우 `scripts/setup-client.sh`를 사용합니다.

### 직접 연결

```bash
sudo ./scripts/setup-client.sh --proxy-host 192.168.1.100 --proxy-port 8080 --install
```

### SSH 터널

```bash
sudo ./scripts/setup-client.sh \
  --proxy-host 192.168.1.100 \
  --proxy-port 8080 \
  --ssh-tunnel \
  --ssh-user myuser \
  --install
```

### 상태 확인 / 제거

```bash
sudo ./scripts/setup-client.sh --status
sudo ./scripts/setup-client.sh --uninstall
sudo ./scripts/setup-client.sh --proxy-host 192.168.1.100 --install --dry-run
```

## 접근 로그

모든 요청은 구조화된 JSON으로 기록됩니다.

```json
{
  "time": "2026-03-09T10:15:32Z",
  "level": "INFO",
  "msg": "access",
  "proxy": {
    "source_ip": "192.168.1.50",
    "method": "CONNECT",
    "domain": "api.github.com:443",
    "status": 200,
    "bytes_in": 0,
    "bytes_out": 15234,
    "duration_ms": 45,
    "allowed": true,
    "reason": "ok"
  }
}
```

| reason | 의미 |
|---|---|
| `ok` | 정상 처리 |
| `domain_not_allowed` | 화이트리스트에 없는 도메인 |
| `proxy_auth_required` | 인증 필요 또는 실패 |
| `target_dial_failed` | 대상 서버 연결 실패 |
| `upstream_roundtrip_failed` | 업스트림 요청 실패 |

## 프로젝트 구조

```
sluice/
├── cmd/
│   ├── proxy/main.go              # 서버 진입점 (기존)
│   └── sluice/main.go             # 서브커맨드 진입점 (server, gateway)
├── internal/
│   ├── config/                    # YAML 설정 로딩 및 검증
│   ├── acl/                       # 도메인 화이트리스트 엔진
│   ├── logger/                    # slog 기반 구조화 접근 로깅
│   ├── proxy/
│   │   ├── handler.go             # HTTP 포워딩, 인증, 헤더 처리
│   │   └── tunnel.go              # HTTPS CONNECT 터널링
│   └── gateway/                   # 순수 Go 투명 프록시 (개발 중)
│       ├── tun.go                 # Linux TUN 디바이스 관리
│       ├── stack.go               # gVisor netstack TCP/IP 스택
│       ├── proxy.go               # HTTP/HTTPS 프록시 포워딩
│       ├── sni.go                 # TLS ClientHello SNI 추출
│       ├── route.go               # netlink 라우팅 관리
│       └── config.go              # 게이트웨이 설정 및 검증
├── configs/config.yaml            # 예제 설정 파일
├── scripts/setup-client.sh        # 클라이언트 설정 스크립트
├── Dockerfile                     # 멀티 스테이지 Docker 이미지
├── docker-entrypoint.sh           # server/run/gateway 모드 엔트리포인트
├── docker-compose.yml             # Compose 예제
└── Makefile
```

## 테스트

```bash
make test
```

## License

MIT
