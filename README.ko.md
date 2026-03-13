# sluice

> English documentation: **[README.md](./README.md)**

**방화벽으로 막힌 Linux 호스트에서도 SSH 포트 하나로 인터넷을 사용하세요.**

`sluice`는 단일 바이너리 기반 프록시 시스템입니다.
- `server`: 포워드 프록시 + 선택적 DoH 엔드포인트
- `agent`(Linux): 투명 인터셉트(TUN/netstack)

차단된 호스트의 트래픽은 SSH 리버스 터널(`ssh -R`)을 통해 프록시 호스트로 전달됩니다.

## 핵심 원리 (어떻게 동작하나)

`sluice agent`의 핵심은 **nftables 마킹 기반 투명 인터셉트**입니다.

에이전트 시작 시 다음을 수행합니다:
1. TUN 디바이스와 userspace TCP/IP 스택을 생성합니다.
2. nftables output 체인 규칙으로 outbound 트래픽을 마킹합니다.
3. 마킹된 패킷에 대해 policy routing(`ip rule` + 전용 route table)을 구성합니다.
4. 마킹된 트래픽을 TUN으로 보내고, netstack이 아래 포트를 처리합니다.
   - DNS `:53` (DoH로 relay)
   - HTTP `:80`
   - HTTPS `:443`
5. 최종 업스트림은 `127.0.0.1:{port}` 프록시 엔드포인트(SSH reverse-tunnel bind)로 전달합니다.

루프 방지를 위해 control-plane 트래픽은 별도 `control-fwmark` bypass 경로를 사용합니다.

## 아키텍처

```text
┌──────────────────┐          ┌──────────────────┐          ┌──────────────┐
│ 차단된 호스트       │──────────│ 프록시 호스트        │──────────│ 인터넷 대상    │
│ (agent, Linux)   │  SSH -R  │ (sluice server)  │  HTTP/S  │              │
│ nft mark + TUN   │  암호화   │ + /dns-query     │          │              │
└──────────────────┘          └──────────────────┘          └──────────────┘
```

## 요구사항

- Agent 모드: Linux + root 권한
- 커널 네트워킹 권한/Capabilities (`NET_ADMIN`, 컨테이너 환경은 `NET_RAW`도 필요할 수 있음)
- TUN 디바이스 사용 가능 (`/dev/net/tun`)
- nftables 지원 (`nf_tables`)
- 프록시 호스트에서 차단 호스트로 SSH 접속 가능

## 빠른 시작

### 1) 설치

```bash
curl -fsSL https://raw.githubusercontent.com/ggos3/sluice/main/scripts/install.sh | bash
```

### 2) 프록시 서버 + 리버스 터널 오케스트레이션 시작

```bash
sluice server user@blocked-host:220 --port 18080
```

터널 대상은 `user@host[:port]` 형식의 위치 인수입니다. SSH 포트를 생략하면 기본값 22가 사용됩니다.

### 3) 차단된 호스트에서 agent 시작 (Linux)

```bash
sudo sluice agent --port 18080
```

### 4) 확인

```bash
curl https://github.com
```

## 데몬 모드

`server`와 `agent` 모두 `--daemon`(또는 `-d`)으로 백그라운드 프로세스로 실행할 수 있습니다:

```bash
sluice server user@blocked-host -d --port 18080
sluice server stop

sudo sluice agent -d --port 18080
sluice agent stop
```

PID 파일은 `/var/run/sluice/`, 로그는 `/var/log/sluice/`에 기록됩니다.

systemd 유닛 파일도 `configs/` 디렉토리에 제공됩니다:

```bash
sudo cp configs/sluice-server.service /etc/systemd/system/
sudo systemctl enable --now sluice-server
```

## 런타임 도메인 관리

서버 실행 중 도메인 규칙을 동적으로 제어할 수 있습니다:

```bash
sluice server deny example.com      # 도메인 차단
sluice server allow example.com     # 도메인 허용
sluice server remove example.com    # 런타임 규칙 제거
sluice server rules                 # 활성 규칙 목록
```

런타임 규칙은 메모리에만 유지되며, 재시작 시 `config.yaml`이 원본이 됩니다.

## DNS 경로

- Agent가 DNS(`:53`)를 인터셉트하고, DoH로 `http://127.0.0.1:{port}/dns-query`에 relay합니다.
- DNS 자기재귀 루프 방지를 위해 control-plane mark bypass를 사용합니다.

## CLI 레퍼런스

```text
sluice server [start] [user@host[:port]] [flags]   프록시 서버 시작
sluice server stop                                  서버 데몬 중지
sluice server deny <domain>                         런타임 도메인 차단
sluice server allow <domain>                        런타임 도메인 허용
sluice server remove <domain>                       런타임 규칙 제거
sluice server rules                                 활성 규칙 목록
sluice agent [start] [flags]                        투명 프록시 에이전트 시작 (Linux)
sluice agent stop                                   에이전트 데몬 중지
sluice run [flags] [-- cmd]                         프록시 환경변수 설정 후 명령 실행
sluice gateway [flags]                              투명 프록시 게이트웨이 (Linux)
sluice version                                      버전 정보
```

`run` 예시:

```bash
sluice run -- curl https://example.com
sluice run --port 18080 -- curl https://example.com
sluice run --proxy-host 127.0.0.1 --port 18080 -- curl https://example.com
```

## 설정

- 서버 설정: `configs/config.yaml`
- 에이전트 제외 규칙: `--no-proxy "*.internal.example,10.0.0.0/8"`
- agent 마킹 제어:
  - `--fwmark`: 인터셉트 데이터 경로용
  - `--control-fwmark`: control-plane bypass용

## Docker

### 서버 이미지

```bash
docker run -d --name sluice-server \
  -v ~/.ssh:/root/.ssh:ro \
  ghcr.io/ggos3/sluice-server \
  user@blocked-host:220 --port 18080
```

### 에이전트 이미지 (Linux host)

```bash
docker run -d --name sluice-agent \
  --net=host \
  --cap-add=NET_ADMIN \
  --cap-add=NET_RAW \
  --device /dev/net/tun:/dev/net/tun \
  ghcr.io/ggos3/sluice-agent \
  --port 18080
```

## E2E 테스트

Docker 기반 firewall + tunnel end-to-end 검증:

```bash
make e2e
```

검증 내용:
- firewall이 agent -> server 직접 접근을 차단하는지
- reverse tunnel이 정상 연결되는지
- 인터셉트된 HTTP/HTTPS 요청이 sluice 경로로 성공하는지

## 수동 설치 / 제거

원샷 설치가 어려운 경우(오프라인 전송, 에어갭 환경 등)에는 릴리스 자산으로 수동 설치하세요:
- `sluice-linux-amd64` 또는 `sluice-linux-arm64` 다운로드
- `sluice-checksums.txt`로 무결성 검증
- `/usr/local/bin/sluice`에 설치

수동 제거:

```bash
sudo rm -f /usr/local/bin/sluice
if [ -L /usr/bin/sluice ] && [ "$(readlink /usr/bin/sluice)" = "/usr/local/bin/sluice" ]; then sudo rm -f /usr/bin/sluice; fi
```

## 프로젝트 구조

- `cmd/sluice/` — CLI 엔트리포인트
- `internal/proxy/` — HTTP/HTTPS 프록시 핵심
- `internal/tunnel/` — SSH 리버스 터널 매니저
- `internal/dns/` — DoH 핸들러
- `internal/gateway/` — 투명 에이전트 핵심(TUN + nftables + policy routing)
- `internal/rules/` — 클라이언트 우회 규칙
- `internal/acl/` — 서버 화이트리스트 + 런타임 deny/allow
- `internal/control/` — Unix 소켓 IPC (런타임 도메인 관리)
- `internal/daemon/` — 프로세스 데몬화 및 PID 파일 관리

## 라이선스

MIT
