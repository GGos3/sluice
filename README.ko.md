# sluice

> English documentation: **[README.md](./README.md)**

**방화벽 환경의 서버에서도, SSH 포트 하나로 인터넷을 사용하세요.**

`sluice`는 서버 측에서 포워드 프록시를 실행하고, 차단된 호스트 측에서 Linux 투명 에이전트를 실행합니다.
에이전트는 HTTP/HTTPS/DNS 트래픽을 가로채어 SSH 리버스 터널(`-R`)로 프록시에 전달합니다.

## 아키텍처

```text
┌──────────────────┐          ┌──────────────────┐          ┌──────────────┐
│ 차단된 호스트       │──────────│ 프록시 호스트        │──────────│ 인터넷 대상    │
│ (agent)          │  SSH -R  │ (sluice server)  │  HTTP/S  │              │
│                  │  암호화   │ + DoH endpoint    │          │              │
└──────────────────┘          └──────────────────┘          └──────────────┘
```

## 주요 기능

- SSH 리버스 터널 오케스트레이션 (`server --tunnel user@host`)
- Linux 투명 에이전트 (TUN/netstack)
- 동일한 sluice 포트에서 DNS-over-HTTPS 제공 (`/dns-query`)
- 도메인 화이트리스트(서버 ACL) 및 선택적 프록시 인증
- 클라이언트 제외 규칙 (`--no-proxy`, 도메인/CIDR)
- 구조화된 접근 로그

## 설치 (원샷)

한 줄 명령으로 미리 빌드된 `sluice` 릴리스 바이너리를 설치할 수 있습니다. 로컬 Go 도구체인은 필요하지 않습니다:

```bash
curl -fsSL https://raw.githubusercontent.com/ggos3/sluice/main/scripts/install.sh | bash
```

설치 후 동일한 `sluice` 바이너리로 서버/에이전트를 실행합니다:

```bash
sluice server --tunnel user@remote-host --ssh-port 220
sudo sluice agent --port 18080
```

설치 스크립트는 현재 OS/아키텍처에 맞는 GitHub Release 바이너리를 내려받고 체크섬을 검증합니다.

설치 스크립트 옵션 예시:

```bash
# 특정 릴리스 버전 설치
curl -fsSL https://raw.githubusercontent.com/ggos3/sluice/main/scripts/install.sh | bash -s -- --version v0.1.0

# 제거
curl -fsSL https://raw.githubusercontent.com/ggos3/sluice/main/scripts/install.sh | bash -s -- uninstall
```

## 설치 (수동)

대상 호스트가 GitHub에 직접 접근할 수 없다면, 다른 머신에서 미리 빌드된 릴리스 바이너리를 내려받아 수동으로 옮겨 설치할 수 있습니다.

```bash
# 인터넷이 되는 머신에서
curl -fsSL https://github.com/ggos3/sluice/releases/download/v0.1.0/sluice-linux-amd64 -o sluice
curl -fsSL https://github.com/ggos3/sluice/releases/download/v0.1.0/sluice-checksums.txt -o sluice-checksums.txt
grep " sluice-linux-amd64$" sluice-checksums.txt | sha256sum -c -

# 방화벽 서버로 바이너리 전송
scp sluice user@firewalled-host:/tmp/sluice

# 방화벽 서버에서 설치
ssh user@firewalled-host 'sudo install -m 0755 /tmp/sluice /usr/local/bin/sluice'
```

설치 후에는 원샷 설치와 동일하게 같은 `sluice` 바이너리를 사용하면 됩니다:

```bash
sluice server --tunnel user@remote-host --ssh-port 220
sudo sluice agent --port 18080
```

## 빠른 시작

### 1) 프록시 서버 + 리버스 터널 시작

```bash
sluice server --tunnel user@remote-host
```

원격 SSH 데몬이 기본 포트(22)가 아닌 포트(예: `220`)를 사용할 경우:

```bash
sluice server --tunnel user@remote-host --ssh-port 220
```

sluice 프록시 포트도 함께 변경할 수 있습니다:

```bash
sluice server --tunnel user@remote-host --ssh-port 220 --port 18080
```

### 2) 차단된 호스트에서 에이전트 시작 (Linux, root)

```bash
sudo sluice agent --port 18080
```

### 3) 확인

```bash
curl https://github.com
```

## 수동 SSH 리버스 터널 (선택)

자동 터널 모드 대신 직접 SSH 리버스 터널을 열 수도 있습니다:

```bash
# 프록시 호스트
./sluice server --port 18080

# 차단된 호스트에서 (SSH 포트 22)
ssh -R 18080:localhost:18080 user@proxy-host -N

# 차단된 호스트에서 (SSH 포트 220)
ssh -p 220 -R 18080:localhost:18080 user@proxy-host -N

# 에이전트
sudo ./sluice agent --port 18080
```

## Docker

### 서버 이미지

```bash
docker run -d --name sluice-server \
  -v ~/.ssh:/root/.ssh:ro \
  ghcr.io/ggos3/sluice-server \
  --tunnel user@remote-host --ssh-port 220
```

### 에이전트 이미지 (Linux host)

```bash
docker run -d --name sluice-agent \
  --net=host \
  --cap-add=NET_ADMIN \
  ghcr.io/ggos3/sluice-agent
```

## 설정 및 모드

- 서버 설정: `configs/config.yaml`
- 에이전트 제외 규칙: `--no-proxy "*.internal.example,10.0.0.0/8"`
- 모드:
  - `server`
  - `gateway` (Linux 전용)
  - `agent` (Linux 전용)
  - `run` (명령 단위 프록시 환경)

`run` 모드 예시:

```bash
./sluice run -- curl https://example.com
./sluice run --port 18080 -- curl https://example.com
./sluice run --proxy-host 127.0.0.1 --port 18080 -- curl https://example.com
```

## 프로젝트 구조

- `cmd/sluice/` — CLI 엔트리포인트
- `internal/proxy/` — HTTP/HTTPS 프록시 핵심
- `internal/tunnel/` — SSH 리버스 터널 매니저
- `internal/dns/` — DoH 핸들러
- `internal/gateway/` — 투명 에이전트 핵심
- `internal/rules/` — 클라이언트 우회 규칙
- `internal/acl/` — 서버 화이트리스트

## 라이선스

MIT
