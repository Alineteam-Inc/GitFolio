# GitFolio

**Your git history, turned into a verified developer portfolio — without sharing a line of code.**

GitFolio is a command-line tool by [Alineteam Inc.](https://aline.team) for macOS and Linux.
It collects metadata of the commits *you* authored and pushed — never file contents — and
[aline.team](https://aline.team) turns it into your developer profile and resume: what you worked
on, when, in which languages and frameworks, and how you used AI coding agents.

Once set up it runs by itself: every `git push` from any tool (terminal, IDE, GUI client or AI agent)
is picked up in the background, without slowing the push down.

> **Status: early development.** Collection, sign-up and login, and syncing to aline.team work against
> the development server. The first release (v0.1.0, Homebrew / curl) follows once the production
> server is ready — see [docs/ROADMAP.md](docs/ROADMAP.md).

[한국어](#한국어)

## Install

Available from the first release (v0.1.0).

**Homebrew** (macOS, Linux)

```sh
brew install Alineteam-Inc/tap/gitfolio
```

**curl** — downloads over HTTPS and verifies the SHA-256 checksum before installing

```sh
curl -fsSL https://raw.githubusercontent.com/Alineteam-Inc/GitFolio/main/install.sh | sh
```

Then run `gitfolio init`.

**From source** (Go 1.27 or later)

```sh
git clone https://github.com/Alineteam-Inc/GitFolio.git
cd GitFolio
go install ./cmd/gitfolio      # installs to ~/go/bin
```

## Usage

```sh
gitfolio init                    # data policy, aline.team sign-up or login, find and choose repositories
gitfolio add ~/code/my-project   # register a repository, collect its commits, install git hooks
gitfolio list                    # registered repositories, commit counts, hook status
gitfolio sync --dry-run          # show exactly what would be sent to aline.team
gitfolio sync                    # send what aline.team does not have yet (pushes are sent automatically)
gitfolio config mask add acme    # hide a customer or internal project name in commit messages
gitfolio remove ~/code/my-project --purge   # unregister, restore previous hooks, delete its data here and on aline.team
gitfolio whoami | logout         # the account this device is logged in to / log out
gitfolio schedule 09:00          # optional daily sync for pushes the hooks missed (off: schedule off)
```

Your profile and resume are available at **[aline.team](https://aline.team)**.

## What is collected

| Collected | Never collected |
|---|---|
| Commit hash, author email, date | Source code and file contents |
| Commit message (masked) and branch name | Full file paths |
| File names and lines added/deleted per file | Remote URLs, hosts and credentials |
| Repository `owner/repo` and service (GitHub, GitLab, …) | Commits by other people |
| Whether an AI agent took part (Claude Code, Codex, Cursor, Copilot, …) | |

- **Masked in commit messages on your computer before anything is stored or sent:** tokens and keys,
  URLs, emails, IP addresses, ticket numbers, and words you block with `gitfolio config mask add`.
  File and repository names are kept as they are.
- **Package manager files** (`package.json`, `pom.xml`, …) are read only with your per-file
  approval, and only to detect dependencies (sending them to aline.team comes later).
- Data is sent to aline.team (Alineteam Inc., Google Cloud, United States) and kept for one
  year from sign-up, renewed while you keep using GitFolio.
  Privacy policy: https://aline.team/privacy

## License

[MIT](LICENSE) © 2026 Alineteam Inc.

---

## 한국어

**git 활동 이력만으로, 코드 한 줄 공개하지 않고 검증된 개발 포트폴리오를 만듭니다.**

GitFolio는 Alineteam Inc.가 만든 macOS·Linux용 CLI입니다. 내가 작성하고 push한 커밋의 메타데이터만
수집하며(파일 내용은 수집하지 않음), [aline.team](https://aline.team)이 이를 개발 프로필과 이력서로
만들어 줍니다. 어떤 작업을 언제 했는지, 어떤 언어·프레임워크를 썼는지, AI 코딩 에이전트를 어떻게
활용했는지가 담깁니다.

한 번 설정하면 터미널·IDE·GUI 클라이언트·AI 에이전트 등 어떤 도구로 `git push`를 하든 백그라운드에서
자동으로 수집되며, push 속도에는 영향을 주지 않습니다.

> **현재 상태: 초기 개발 중.** 수집, 가입·로그인, aline.team 동기화가 개발 서버에서 동작합니다.
> 첫 릴리스(v0.1.0, Homebrew·curl 설치)는 운영 서버 준비 후 나옵니다 ([docs/ROADMAP.md](docs/ROADMAP.md)).

### 설치

첫 릴리스(v0.1.0)부터 사용할 수 있습니다.

- **Homebrew** (macOS, Linux): `brew install Alineteam-Inc/tap/gitfolio`
- **curl** (HTTPS로 받고 SHA-256을 검증한 뒤 설치):
  ```sh
  curl -fsSL https://raw.githubusercontent.com/Alineteam-Inc/GitFolio/main/install.sh | sh
  ```
- 설치 후 `gitfolio init`을 실행하세요.
- **소스에서 설치** (Go 1.27 이상):
  ```sh
  git clone https://github.com/Alineteam-Inc/GitFolio.git
  cd GitFolio
  go install ./cmd/gitfolio      # ~/go/bin 에 설치
  ```

### 사용법

```sh
gitfolio init                    # 데이터 정책, aline.team 가입·로그인, 저장소 찾기·선택
gitfolio add ~/code/my-project   # 저장소 등록, 커밋 수집, git 훅 설치
gitfolio list                    # 등록 저장소, 커밋 수, 훅 상태
gitfolio sync --dry-run          # aline.team에 보낼 내용을 그대로 미리 보기
gitfolio sync                    # aline.team에 없는 것만 전송 (push하면 자동 전송)
gitfolio config mask add 고객사A  # 커밋 메시지 속 고객사·사내 프로젝트명을 가림
gitfolio remove ~/code/my-project --purge   # 등록 해제, 기존 훅 복원, 이 컴퓨터와 aline.team의 데이터 삭제
gitfolio whoami | logout         # 이 기기의 로그인 계정 확인 / 로그아웃
gitfolio schedule 09:00          # 훅이 놓친 push를 매일 보완하는 예약 동기화 (선택, 해제: schedule off)
```

내 프로필과 이력서는 **[aline.team](https://aline.team)** 에서 직접 확인할 수 있습니다.

### 수집 항목

- **수집:** 커밋 해시, 작성자 이메일, 시점, 커밋 메시지(마스킹), 브랜치 이름, 파일명과 파일별 추가·삭제 줄 수,
  저장소 `소유자/저장소`와 git 서비스, AI 에이전트 참여 여부
- **수집하지 않음:** 소스 코드·파일 내용, 전체 경로, 원격 URL·호스트·인증 정보, 다른 사람의 커밋
- 커밋 메시지 속 토큰·키, URL, 이메일, IP, 티켓 번호, 등록한 금지어는 **이 컴퓨터에서 가린 뒤** 저장·전송합니다. 파일명·저장소 이름은 그대로 둡니다.
- 패키지 매니저 파일은 파일별로 승인한 경우에만, 의존성 파악 용도로만 읽습니다 (aline.team 전송은 추후).
- 데이터는 aline.team(Alineteam Inc., Google Cloud, 미국)으로 전송되며, 가입일로부터 1년간
  보관되고 계속 이용 중이면 자동 연장됩니다. 개인정보처리방침: https://aline.team/privacy

### 라이선스

[MIT](LICENSE) © 2026 Alineteam Inc.
