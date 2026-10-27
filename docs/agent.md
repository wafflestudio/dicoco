# OpenCode 에이전트 운영 안내

## 실행 구조와 사용법

```text
컨테이너 1개 (메모리 제한은 아래 프로세스들의 합계)
  Go Discord 봇
    └─ opencode serve --hostname 127.0.0.1 --port 0
         └─ OpenCode 2의 기본 모델로 조사
```

봇이 OpenCode를 자식 프로세스로 시작하고, 생성된 로컬 주소와 인증 비밀번호를 메모리에서 받아 HTTP로 호출한다.
별도 Node/Python 중계, 공개 포트, Actions 분석 작업, OpenCode 전용 컨테이너는 필요 없다.
서버 비밀번호와 모델 응답 원문은 시작 로그에 출력하지 않는다. 종료 시 봇이 시작한 OpenCode도 종료한다.

- 서버: 기존 Secret의 `admin_2026_channel_id`가 가리키는 **admin 채널과 그 아래 스레드에서만** 봇 계정을 `@봇이름`으로 멘션한다. 새 채널 ID 설정은 필요 없다. 로그인한 봇의 사용자 ID와 메시지의 멘션 ID 목록을 비교한다.
- 일반 채널에서 시작하면 공개 스레드를 만든다. 기존 스레드에서 시작하면 새 스레드를 만들지 않고 그곳에서 답한다. 처음 호출할 때는 해당 스레드의 직전 메시지 최대 20개와 조회 가능한 원본 글(각 2,000자까지)을 읽어 문맥으로 전달한다.
- 서버 스레드에서는 **후속 요청도 매번 멘션**해야 한다. 멘션 없는 대화에는 응답하지 않는다. 후속 멘션 시 최근 20개 메시지 중 이전 호출 이후 추가된 사람들의 대화를 전달하고, 이전 에이전트 문맥은 같은 OpenCode 세션으로 이어 간다. 매 호출 사이 메시지가 20개를 넘으면 오래된 추가 대화는 누락될 수 있다.
- DM: 같은 봇에게 개인 메시지를 보내면 멘션 없이 처리한다. DM 채널 하나가 별도 대화 하나이며, 서버 스레드나 다른 사용자의 DM과 세션을 공유하지 않는다.
- 봇·웹훅 메시지는 무시한다. 지정 채널 밖의 채널·스레드에서는 작업을 시작하지 않는다.
- 서버 스레드와 DM에 대한 전체 동시 분석은 **1건**, 접수 가능한 실행·대기 요청 합계는 **32건**, 실행 제한은 요청당 **10분**이다. 진행 중 후속 요청은 대기한다.
- 지금은 텍스트 요청만 받는다. 첨부파일 자동 다운로드·문서 파싱은 구현하지 않았다.

예시:

```text
지정 채널: @봇 dicoco에서 멘션을 처리하는 부분을 찾아줘
생성된 스레드: @봇 DM은 어디서 처리해?
개인 DM: 이 저장소 구조를 설명해줘
```

DM을 보낼 수 있는 사용자도 준비된 저장소를 조사할 수 있다. 현재 별도의 Discord 역할·사용자 허용 목록은 없다.

## 환경변수 전체

| 설정 | 기본값 | 설명 |
|---|---|---|
| `DISCORD_TOKEN_FILE` | `/var/run/secrets/discord-bot/token` | 기존 봇 토큰이 들어 있는 파일. **토큰 값이 아니라 파일 경로**를 지정한다. |
| `AGENT_ENABLED` | `true` | 기본으로 OpenCode 에이전트를 실행한다. `false`로 명시하면 에이전트를 끈다. |
| `admin_2026_channel_id` (기존 Secret 파일) | 토큰 파일과 같은 디렉터리의 `admin_2026_channel_id` | 서버에서 허용할 admin 채널 ID. 기존 설정을 재사용한다. 파일이 없어도 DM은 동작하며, 서버 채널의 요청만 받지 않는다. `AGENT_CHANNEL_ID` 환경변수는 사용하지 않는다. |
| `AGENT_ONLY` | `false` | `true`면 에이전트만 등록한다. 로컬 테스트 시 기존 Notion·게임 등의 설정 없이 DM부터 실행하는 용도. |
| `OPENCODE_DIRECTORY` | 로컬 현재 디렉터리, 이미지 `/workspace/dicoco` | **OpenCode가 실행되는 환경 기준** 조사 대상 디렉터리. 로컬에서는 절대 경로로 변환한다. 외부 서버를 사용할 때도 그 서버의 절대 경로를 넣는다. |
| `OPENCODE_BINARY` | `opencode` | 봇이 실행할 OpenCode 2 실행 파일 경로. 이미지에는 빌드 시점의 공식 최신 v2가 설치된다. |
| `OPENCODE_URL` | 없음 | 비우면 봇이 로컬 OpenCode를 시작한다. 이미 띄운 서버를 쓰려는 경우에만 URL을 지정한다. 이 경우 서버 시작·종료는 외부에서 관리한다. |
| `OPENCODE_PASSWORD_FILE` | 없음 | 외부 서버의 비밀번호 파일. `OPENCODE_URL`과 함께 사용하며 Basic 인증의 사용자 이름은 `opencode`다. 봇이 시작하는 로컬 서버는 비밀번호를 자동으로 받으므로 설정하지 않는다. |
| `GITHUB_APP_ID_FILE` | `/var/run/secrets/discord-bot/github_app_id` | 기존 OCI Vault/ExternalSecret에서 마운트하는 GitHub App ID 파일. 현재 App ID 값은 `5490993`. |
| `GITHUB_APP_PRIVATE_KEY_FILE` | `/var/run/secrets/github-app/private-key.pem` | 별도 `dicoco-github-app` Kubernetes Secret에서 마운트하는 `.pem` 개인 키 파일. |
| `XDG_DATA_HOME` | 이미지 `/data/opencode/data` | OpenCode DB·세션 등 데이터 저장 경로. |
| `XDG_STATE_HOME` | 이미지 `/data/opencode/state` | OpenCode 상태·로그 저장 경로. |
| `XDG_CONFIG_HOME` | 이미지 `/data/opencode/config` | OpenCode 설정 저장 경로. |
| `XDG_CACHE_HOME` | 이미지 `/data/opencode/cache` | OpenCode 캐시 저장 경로. |

`AGENT_ONLY=false`이면 기존 Notion 등 기능의 Secret 파일도 기존과 같이 필요하다.
예전의 단순 멘션 ping과 DM 수신 확인 기능은 소스만 참고용으로 남기고 앱에 등록하지 않는다.

모델 이름이나 모델 API 키는 봇의 필수 설정이 아니다. 세션 생성 때 모델을 지정하지 않아 **OpenCode가 선택하는 기본 모델**을 사용한다.
로컬 OpenCode와 배포 컨테이너는 설정·데이터 디렉터리가 별개다. 로컬에서 바꾼 모델·로그인·설정이 배포 서버로 자동 복사되지는 않는다.
기본 모델의 무료 제공·호출 제한은 OpenCode 제공자 정책을 따른다. 봇에 유료 모델 자동 전환이나 자동 충전 기능은 없다.

## Discord 권한

Discord Developer Portal의 Bot 설정에서 **Message Content Intent**를 켠다. 코드에는 Guild Messages와 Direct Messages 이벤트 구독이 이미 설정되어 있다.

지정 채널에서 봇 역할에 다음 권한을 준다.

- View Channel
- Send Messages
- Create Public Threads
- Send Messages in Threads
- Read Message History

admin 채널은 기존 `/var/run/secrets/discord-bot/admin_2026_channel_id` 파일의 값을 사용한다.
봇 이름을 바꿔도 ID로 멘션을 확인하므로 별도 변경은 필요 없다. DM 수신은 사용자의 서버 개인 정보 설정에 의해 막힐 수 있다.

## 로컬 실행

이미 설치한 OpenCode 2를 사용한다. 아래 명령은 Discord에 실제 접속하므로 같은 토큰으로 배포 봇과 로컬 봇을 동시에 실행하지 않는다.

```sh
DISCORD_TOKEN_FILE=/절대/경로/봇토큰파일 \
AGENT_ONLY=true \
OPENCODE_DIRECTORY="$PWD" \
go run ./cmd/bot
```

이 명령으로 DM을 사용할 수 있다. 서버에서도 사용하려면 토큰 파일 옆에 `admin_2026_channel_id` 파일로 admin 채널 ID를 준비한다.

봇은 세션 연결과 대기열을 메모리에만 둔다. 로컬 OpenCode는 기존 사용자 데이터 디렉터리를 사용하므로 자체 DB·실행 로그는 남을 수 있지만, 봇은 재시작 뒤 이전 세션을 복구하지 않는다.
로컬 TUI를 닫아도 에이전트는 봇이 별도로 시작한 OpenCode 프로세스로 동작한다.

## 컨테이너 배포

Dockerfile에 Go 실행 파일, 빌드 시점의 **공식 최신 OpenCode v2**, Git, `dicoco` 소스가 포함되어 있다.
이미지 안의 소스는 root 소유이고 실행 사용자는 **UID 10001**이므로 에이전트가 원본 소스를 수정할 수 없다.
배포 워크플로는 빌드마다 공식 최신 버전 API를 조회하고 `OPENCODE_VERSION` 빌드 인자로 전달한다. 최신 버전이 그대로라면 설치 레이어를 재사용하고, 새 버전이면 새 바이너리를 설치한다.
컨테이너 실행 중 자동 업데이트는 하지 않으며 다음 이미지 빌드·배포 때 최신 버전이 적용된다.
`main` push 외에도 **매주 월요일 오전 9시(한국 시간)**에 같은 테스트·빌드·배포 흐름을 자동 실행한다. GitHub 스케줄 실행은 지연될 수 있다.
주간 실행은 커밋이 같아도 고유한 `SHA-weekly-실행ID-시도번호` 이미지 태그를 사용하여 GitOps 이미지 변경과 Argo CD 재배포가 실제로 일어나게 한다. 이 워크플로 변경이 기본 브랜치에 반영된 뒤부터 스케줄이 활성화된다.
수동 로컬 이미지 빌드에서 최신 버전을 다시 받고 싶으면 `--no-cache-filter opencode`를 지정하거나, 최신 버전을 확인해 `--build-arg OPENCODE_VERSION=버전`을 지정한다.
Go API 연동은 OpenCode 2.1.4에서 실측 검증했고, 최신 버전의 API 변경에 따라 재검증이 필요할 수 있다.

기존 배포에 필요한 변경:

1. 에이전트는 기본으로 활성화된다. 기존 기능도 사용할 경우 `AGENT_ONLY`는 설정하지 않는다. 기존 `admin_2026_channel_id` Secret 파일을 재사용하므로 새 채널 환경변수는 필요 없다.
2. 기존 Discord 토큰 Secret 마운트를 유지한다.
3. **PVC는 만들지 않는다.** `/data`는 OpenCode가 실행 중 DB·캐시를 쓰는 컨테이너 내부 경로이며, 컨테이너 교체 때 사라져도 된다. 기본 이미지 설정대로라면 별도 마운트가 필요 없다. 배포에서 `readOnlyRootFilesystem: true`를 강제한다면 `/data`를 UID 10001이 쓸 수 있는 `emptyDir`로 제공한다.
4. 컨테이너 메모리 제한을 현재 정한 `1Gi`로 둔다. 동시 분석은 한 건이지만 1Gi 안에서 항상 완료된다는 보장은 없으므로 경고와 OOMKilled 여부를 확인한다.
5. **replicas는 1개**로 유지한다. 인메모리 큐·세션 연결은 봇 인스턴스끼리 공유하지 않는다. 롤링 업데이트 시에도 같은 토큰의 두 인스턴스가 동시에 요청을 받지 않도록 `Recreate` 또는 이에 준하는 전략을 쓴다.
6. OpenCode 로컬 포트를 Service나 Ingress로 노출할 필요는 없다. Discord와 모델 제공자·웹 자료를 향한 아웃바운드 연결은 필요하다.

Kubernetes 에이전트 관련 설정 예시(기존 Deployment의 해당 항목에 병합):

```yaml
spec:
  replicas: 1
  strategy:
    type: Recreate
  template:
    spec:
      containers:
        - name: bot # 실제 기존 컨테이너 이름에 맞춤
          resources:
            limits:
              memory: 1Gi
          # 기존 토큰·다른 Secret 마운트는 유지. PVC는 추가하지 않음.
```

이 저장소의 배포 워크플로는 이미지를 빌드하고 외부 GitOps 저장소의 태그를 바꾼다.
Deployment 본문은 이 저장소에 없으므로 위 설정은 **외부 GitOps 저장소에서 따로 적용**해야 한다.
컨테이너 시작 시 별도 `opencode serve` 명령을 추가하지 않는다. Go 봇이 실행한다.

## 저장소 접근과 현재 범위

첫 대상은 이미지 빌드 시 포함한 `dicoco` 소스다. 실행 중 GitHub에서 최신 코드를 자동으로 pull하지 않으며, 새 이미지를 배포하면 소스도 바뀐다.
다른 저장소는 실행 환경에 준비하고 `OPENCODE_DIRECTORY`를 바꾸거나 그 경로에 읽기 전용으로 마운트한다.
GitHub 접근은 컨테이너에 설치한 **일반 `gh` CLI**를 OpenCode가 직접 실행하는 방식이다. 별도의 PR 처리기나 GitHub MCP는 없다.
조직은 코드에서 `wafflestudio`로 지정하므로 `github_org`나 Installation ID를 입력할 필요가 없다.

GitHub App ID와 키 파일이 모두 없으면 인증 연결을 생략한다. 하나만 있거나 파일이 잘못되면 시작 오류로 알린다.
둘 다 있으면 Go가 App JWT를 만들고 조직의 Installation ID를 자동 조회한 뒤, Contents 읽기·Pull requests 쓰기 권한의 짧은 수명 토큰을 발급한다.
`gh` 전용 임시 설정 파일(`hosts.yml`, 소유자만 읽기 가능)에 저장하고 만료 5분 전에 갱신한다. 원본 개인 키는 OpenCode에 프롬프트나 환경변수로 전달하지 않는다.
OpenCode에는 해당 디렉터리를 가리키는 `GH_CONFIG_DIR`을 전달한다. `gh`는 실행할 때마다 현재 토큰을 읽으므로 갱신 때 OpenCode를 재시작하지 않는다.
관리형 인증을 쓸 때는 외부 OpenCode URL 대신 같은 컨테이너에서 실행하는 기본 구성을 사용한다.

현재 준비한 Secret 배치:

```text
기존 OCI Vault → ExternalSecret → discord-bot Secret
  /var/run/secrets/discord-bot/github_app_id

kubectl로 생성한 dicoco-github-app Secret → GitOps Deployment 마운트
  /var/run/secrets/github-app/private-key.pem
```

App을 `wafflestudio`에 설치하고 대상 저장소에 접근 권한을 준다. App 권한은 Contents Read-only, Pull requests Read & write가 필요하다.
키 파일은 실제 PEM 줄바꿈을 가진 파일로 마운트한다. `github_private_key`를 Vault JSON 안에 추가할 필요는 없다.
토큰 갱신 로그에는 조직과 만료 시간만 남기며 개인 키·토큰·인증 응답 본문은 출력하지 않는다.

예시 요청:

```text
@봇 https://github.com/wafflestudio/REPO/pull/42 의 변경을 설명해줘
@봇 이 PR을 리뷰해서 GitHub에도 COMMENT 리뷰로 달아줘 https://github.com/wafflestudio/REPO/pull/42
@봇 REPO에서 로그인 처리 코드를 찾아줘
```

OpenCode가 `gh pr view`, `gh pr diff`, `gh api --method GET`으로 관련 자료를 읽고, 명시적으로 게시를 요청받으면 `gh pr review URL --comment --body ...`로 등록한다.
단순 코드 질문이나 PR 조사만으로는 GitHub에 게시하지 않도록 지침을 전달한다.
커밋 SHA 고정·게시 전 재확인·중복 게시 확인은 에이전트에게 전달하는 지침이며, Go가 별도 PR 워크플로로 강제 검증하지 않는다.

각 OpenCode 세션에는 파일 읽기·glob·grep·웹 fetch·웹 검색과 필요한 `gh` 셸 명령을 허용하는 권한을 지정한다.
셸 전체를 여는 대신 PR·저장소 조회, GET API 호출과 COMMENT 리뷰 명령을 허용한다. 수정·서브에이전트·MCP·프로젝트 밖 접근은 허용하지 않는다. 대상 코드를 실행해서 테스트하지는 않는다.
OpenCode의 도구 권한은 실행 시 검사되는 규칙이며 프롬프트와 별개다. 다만 셸 분석을 포함한 애플리케이션 권한이므로 OS 샌드박스와 같은 격리를 보장하지는 않는다.
웹 검색은 OpenCode와 해당 제공자의 실제 지원 상태에 따라 제한될 수 있다.
대상 코드에 없는 정보나 제공되지 않은 PR을 읽은 것처럼 답하지 않도록 지침을 전달한다.

임의의 외부 프로젝트 설정·플러그인을 격리해서 로딩하는 기능은 아직 없다. 처음에는 이 이미지에 포함한 소스 등 **신뢰하는 작업 디렉터리**만 사용한다.

## 대기열·장애 처리

OpenCode 세션은 별도의 유휴 만료 시간 없이 봇 실행 중 유지되며, 봇 재시작 때 연결을 초기화한다. 10분 제한은 대화 수명이 아니라 **요청 한 건의 분석 시간**이다.
봇이 새로 만드는 Discord 스레드는 활동이 없으면 **24시간 뒤 자동 보관**되도록 요청한다. 기존 스레드의 자동 보관 설정은 바꾸지 않는다. 보관은 Discord 기능이며 OpenCode 세션 만료와 별개다.

장기 기억이나 디스크 기반 봇 상태 복구는 없다. 대기열·세션 연결은 메모리에만 있고 업데이트·재시작 때 초기화한다.
디스코드의 스레드와 메시지는 디스코드에 그대로 남는다. 기존 스레드에서 **다시 멘션하면** 최근 대화를 읽어 새 세션을 만든다. 재시작 뒤 태그 없는 메시지만으로 이전 스레드 처리를 재개하지는 않는다.
DM은 다음 메시지부터 새 세션을 시작한다. 이전 OpenCode 내부 문맥이나 대기 요청은 복구하지 않는다.
OpenCode 중단 여부를 확인하지 못하면 추가 분석을 멈춰 동시 실행이 늘어나는 것을 막는다. 연결을 복구하고 봇을 재시작한다.

최근 256개 요청은 Discord 메시지 ID로 중복을 막는다. PR·커밋 단위 중복 검사는 아직 없다.
타임아웃·모델 오류·부분 응답은 실패로 구분하고 성공한 분석 결과처럼 보내지 않는다.
Discord 전송 실패는 `delivery_failed`로 기록하며 자동 재게시하지 않는다. Discord 메시지 삭제/수정과 오프라인 중의 누락 메시지 재수집은 지원하지 않는다.
오류는 디스코드에 간단히 안내하고, 요청 ID와 상세 오류·HTTP 상태는 서버 로그에 남긴다. 제공자가 구조화된 모델 오류를 반환하면 그 상태와 오류 메시지도 로그에 남긴다.

## 메모리 로그

Linux 컨테이너에서 **10초마다**, cgroup v2의 `memory.current` 또는 v1의 `memory.usage_in_bytes`를 읽는다.
사용량이 낮아도 **1분마다** 현재 사용량을 남긴다(첫 로그는 시작 후 약 10초 뒤).

```text
[memory] container_memory=354.2MiB source=cgroup_v2
```

**800MiB 이상이면 매 측정마다(10초 간격)** 다음 형태의 경고 로그를 남긴다.

```text
[memory] HIGH container_memory=824.6MiB threshold=800MiB source=cgroup_v2
```

여기에는 Go·OpenCode·자식 프로세스와 cgroup이 계산하는 파일 페이지 캐시가 포함된다. Go 힙 사용량만의 값이 아니다.
800MiB는 838,860,800바이트이고, 1Gi 제한의 약 78%다.
이 기능은 경고만 남기며 자동 중단·재시작·메모리 회수는 하지 않는다.
macOS 로컬 실행이나 cgroup 카운터가 없는 환경에서는 사용 불가 로그를 한 번 남기고 감시를 종료한다.

## 검증

```sh
go test ./...
go vet ./...
go build -o /tmp/dicoco ./cmd/bot
```

일반 테스트는 모델을 호출하지 않는다. 로컬 HTTP 가짜 서버와 가짜 Discord/에이전트로 요청 형식·멘션 필터·기존 스레드 호출·DM 세션 분리·순차 처리·중복·타임아웃을 검증한다.

일반 테스트는 러너에 미리 설치된 `gh`에 의존하지 않는다. 실제 CLI가 생성한 인증 파일과 갱신 토큰을 읽는 호환성 검사는 필요할 때 별도로 실행한다(실제 GitHub 요청 없음):

```sh
DICOCO_GH_CLI_TEST=1 go test ./internal/githubauth -run TestGHConfigCLI -v -count=1
```

실제 OpenCode 연동을 직접 다시 검증하고 싶을 때만:

```sh
DICOCO_OPENCODE_LIVE_TEST=1 go test ./internal/opencode -run TestLiveOpenCode -v -count=1
```

이 테스트는 이미 설치한 OpenCode를 사용하고 모델에 임시 샘플 파일을 읽게 한다. 별도 프로그램 설치나 Docker 이미지 다운로드는 하지 않는다.
후속 대화와 실제 수정·셸 권한 거부를 확인한 뒤 테스트 세션·임시 파일·프로세스를 정리한다. Go 빌드 캐시와 OpenCode 실행 로그는 남을 수 있다.
