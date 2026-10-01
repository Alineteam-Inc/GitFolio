# GitFolio

**Your git history, turned into a verified developer portfolio — without sharing a line of code.**

GitFolio is a command-line tool by [Alineteam Inc.](https://aline.team) for macOS, Linux and Windows.
It collects metadata of the commits *you* authored and pushed — never file contents — and
[aline.team](https://aline.team) turns it into your developer profile and resume: what you worked
on, when, in which languages and frameworks, and how you used AI coding agents.

Once set up it runs by itself: every `git push` from any tool (terminal, IDE, GUI client or AI agent)
is picked up in the background, without slowing the push down.

> **Status:** first release, [v0.1.0](https://github.com/Alineteam-Inc/GitFolio/releases/tag/v0.1.0) —
> see [docs/ROADMAP.md](docs/ROADMAP.md) for what comes next.

[한국어](#한국어)

## Install

Requires `git`.

**Homebrew** (macOS, Linux)

```sh
brew install Alineteam-Inc/tap/gitfolio
```

**curl** (macOS, Linux) — downloads over HTTPS and verifies the SHA-256 checksum before installing
(to `/usr/local/bin` if writable, else `~/.local/bin`)

```sh
curl -fsSL https://raw.githubusercontent.com/Alineteam-Inc/GitFolio/main/install.sh | sh
```

**PowerShell** (Windows) — the same checks; installs to `%LOCALAPPDATA%\Programs\GitFolio` and adds it to your PATH

```powershell
irm https://raw.githubusercontent.com/Alineteam-Inc/GitFolio/main/install.ps1 | iex
```

GitFolio checks for a new release once a day when you run it in a terminal and asks before
updating. To update by hand: `brew update && brew upgrade --cask gitfolio`, or run the curl /
PowerShell command again.

**From source** (Go 1.27 or later, for contributors)

```sh
git clone https://github.com/Alineteam-Inc/GitFolio.git
cd GitFolio
go install ./cmd/gitfolio      # installs to ~/go/bin
```

## Get started

```sh
gitfolio init
```

`init` walks you through everything once:

1. Shows the data policy (below).
2. Signs you up or logs you in to aline.team with a code sent to your email.
   Nothing is collected before you log in.
3. Finds the git repositories under your code folders; you choose which ones to collect.
4. Asks about optional dependency detection and a daily sync.
5. Installs git hooks in the chosen repositories and sends their history to aline.team.

From then on, each `git push` is sent automatically.

## See your profile on aline.team

1. Log in at **[aline.team](https://aline.team)** with the account you used in `gitfolio init`
   (`gitfolio whoami` shows it).
2. Signed up from the CLI? Set a website password first with the link emailed to you. It works once
   within 24 hours; after that, use "Forgot password" on the website.
3. Open your profile at **[aline.team/profile](https://aline.team/profile)**. It and your resume are
   built from what GitFolio sends, and update after aline.team's next analysis run, not right after each push.

- Commits appear under your **primary work email** (`gitfolio email`). Changing it applies to
  repositories added after the change; repositories already on aline.team keep their first email.
- If you also connected the same repository on the aline.team website, GitFolio's commits join that
  repository.

## Data policy

**Only metadata of your own pushed commits, from repositories you chose, after you logged in.**
"Your own" means commits by your work emails (`gitfolio email`) or the repository's git email.

| Sent to aline.team | Never collected |
|---|---|
| Repository service (GitHub, GitLab, …) and `owner/repo` | Source code and file contents |
| Your primary work email | Where the repository is on your computer |
| Commit hash, branch name, time (with time zone offset) | Remote URLs, hosts and credentials (git, `gh`, …) |
| Commit message, masked on your computer | Commits by other people |
| File paths in the repository, lines added/deleted, whether the commit created the file | |
| Whether an AI agent took part (Claude Code, Codex, Cursor, Copilot, …) | |
| For files you created: when someone else first changed them (time only, not who) | |

- **Masking:** before anything is stored or sent, commit messages lose tokens and keys, URLs, emails,
  IP addresses, ticket numbers and words you block with `gitfolio config mask add`. File paths,
  branch and repository names are kept as they are.
- **Package manager files** (`package.json`, `pom.xml`, …) are read only with your per-file approval,
  only to detect dependencies, and only when you ask: right after you approve them, with
  `gitfolio deps scan`, or by scan, sync and the daily sync if you turn that on (`gitfolio deps auto on`).
  Pushes never read them. Dependencies are not sent yet.
- **On your computer:** data and the login token are kept in your user config folder
  (`~/Library/Application Support/gitfolio`, `~/.config/gitfolio` or `%AppData%\gitfolio`),
  readable only by you. The token is never printed or logged.
- **Transfer and retention:** HTTPS only, to aline.team (Alineteam Inc., Google Cloud,
  United States). Kept for one year from sign-up, renewed while you keep using GitFolio.
  Signing up means agreeing to the [Terms of Service](https://aline.team/terms) and the
  [Privacy Policy](https://aline.team/privacy).
- **You stay in control:** `gitfolio sync --dry-run` prints exactly what would be sent,
  `gitfolio config autosync off` stops sending on push, and `gitfolio remove <path> --purge`
  deletes a repository's data here and on aline.team.

### Check it yourself

You don't have to take our word for it.

- **What it reads.** GitFolio reads your repositories only by running `git`, and reads file contents
  only with `git cat-file`. git itself records every command GitFolio runs (`GIT_TRACE`, on by
  default, up to 1 MB with the oldest runs removed first):
  ```sh
  gitfolio history               # per run: the gitfolio command, then each git command
  gitfolio history --all | grep cat-file    # only the package manager files you approved
  ```
  `gitfolio config git-history off` stops and deletes the record; `config git-history-size 4MB`
  changes the limit. To keep a trace of your own instead:
  ```sh
  GIT_TRACE=$PWD/git-trace.txt gitfolio deps scan --all
  grep cat-file git-trace.txt
  ```
  On Windows PowerShell: `$env:GIT_TRACE="$PWD\git-trace.txt"; gitfolio deps scan --all; Select-String cat-file git-trace.txt`,
  then `Remove-Item Env:GIT_TRACE`. To see every file GitFolio opens, use `strace -f -e trace=openat`
  (Linux), `sudo fs_usage -w -f filesys gitfolio` (macOS) or Process Monitor (Windows).
- **What it sends.** `gitfolio sync --dry-run` prints the requests exactly as they would be sent.
- **What you installed.** Releases after v0.1.0 carry signed build provenance.
  `gh attestation verify gitfolio_darwin_arm64.tar.gz -R Alineteam-Inc/GitFolio` proves that the
  archive was built by this repository's release workflow from the tagged public source. That source
  passed CI on macOS, Linux and Windows, including a test that fails if a file you did not approve is read.

## Commands

```sh
gitfolio init                    # data policy, aline.team sign-up or login, find and choose repositories
gitfolio add ~/code/my-project   # register a repository, collect its commits, install git hooks
gitfolio list                    # registered repositories, commit counts, hook status
gitfolio sync --dry-run          # show exactly what would be sent to aline.team
gitfolio sync                    # send what aline.team does not have yet (pushes are sent automatically)
gitfolio config mask add acme    # hide a customer or internal project name in commit messages
gitfolio remove ~/code/my-project --purge   # unregister, restore previous hooks, delete its data here and on aline.team
gitfolio whoami | logout         # the account this device is logged in to / log out
gitfolio email add me@work.com   # another work email of yours (email primary <e>: the one aline.team shows)
gitfolio schedule 09:00          # optional daily sync for pushes the hooks missed (off: schedule off)
gitfolio deps scan --all         # read the approved package manager files again (dependencies)
gitfolio history                 # the git commands gitfolio ran, as git itself recorded them
gitfolio help                    # all commands
```

## License

[MIT](LICENSE) © 2026 Alineteam Inc.

---

## 한국어

**git 활동 이력만으로, 코드 한 줄 공개하지 않고 검증된 개발 포트폴리오를 만듭니다.**

GitFolio는 Alineteam Inc.가 만든 macOS·Linux·Windows용 CLI입니다. 내가 작성하고 push한 커밋의 메타데이터만
수집하며(파일 내용은 수집하지 않음), [aline.team](https://aline.team)이 이를 개발 프로필과 이력서로
만들어 줍니다. 어떤 작업을 언제 했는지, 어떤 언어·프레임워크를 썼는지, AI 코딩 에이전트를 어떻게
활용했는지가 담깁니다.

한 번 설정하면 터미널·IDE·GUI 클라이언트·AI 에이전트 등 어떤 도구로 `git push`를 하든 백그라운드에서
자동으로 수집되며, push 속도에는 영향을 주지 않습니다.

> **현재 상태:** 첫 릴리스 [v0.1.0](https://github.com/Alineteam-Inc/GitFolio/releases/tag/v0.1.0) — 이후 계획은 [docs/ROADMAP.md](docs/ROADMAP.md).

### 설치

`git`이 필요합니다.

- **Homebrew** (macOS, Linux): `brew install Alineteam-Inc/tap/gitfolio`
- **curl** (macOS, Linux — HTTPS로 받고 SHA-256을 검증한 뒤 `/usr/local/bin`, 쓸 수 없으면 `~/.local/bin`에 설치):
  ```sh
  curl -fsSL https://raw.githubusercontent.com/Alineteam-Inc/GitFolio/main/install.sh | sh
  ```
- **PowerShell** (Windows — 같은 검증 후 `%LOCALAPPDATA%\Programs\GitFolio`에 설치하고 PATH에 추가):
  ```powershell
  irm https://raw.githubusercontent.com/Alineteam-Inc/GitFolio/main/install.ps1 | iex
  ```
- **업데이트:** 터미널에서 실행하면 하루 한 번 새 버전을 확인하고, 업데이트할지 묻습니다. 직접 하려면
  `brew update && brew upgrade --cask gitfolio`, 또는 curl·PowerShell 명령을 다시 실행하세요.
- **소스에서 설치** (Go 1.27 이상, 기여자용):
  ```sh
  git clone https://github.com/Alineteam-Inc/GitFolio.git
  cd GitFolio
  go install ./cmd/gitfolio      # ~/go/bin 에 설치
  ```

### 시작하기

```sh
gitfolio init
```

`init` 한 번으로 설정이 끝납니다.

1. 데이터 정책(아래)을 보여 줍니다.
2. 이메일로 받은 확인 코드로 aline.team에 가입하거나 로그인합니다. 로그인 전에는 아무것도 수집하지 않습니다.
3. 코드 폴더에서 git 저장소를 찾고, 수집할 저장소를 고릅니다.
4. 의존성 분석과 예약 동기화(선택)를 묻습니다.
5. 고른 저장소에 git 훅을 설치하고 지금까지의 이력을 aline.team에 보냅니다.

이후에는 `git push`할 때마다 자동으로 전송됩니다.

### aline.team에서 내 프로필 보기

1. **[aline.team](https://aline.team)** 에 `gitfolio init`에서 쓴 계정으로 로그인합니다 (`gitfolio whoami`로 확인).
2. CLI에서 가입했다면 먼저 메일로 받은 링크로 웹 비밀번호를 설정합니다. 링크는 24시간 안에 한 번 쓸 수 있고,
   만료되면 웹의 "비밀번호 찾기"를 쓰세요.
3. **[aline.team/profile](https://aline.team/profile)** 에서 내 프로필을 봅니다. 프로필과 이력서는 GitFolio가 보낸 데이터로
   만들어지며, push 직후가 아니라 aline.team의 다음 분석 때 반영됩니다.

- 커밋은 **대표 작업 이메일**(`gitfolio email`의 ★)로 집계됩니다. 대표 이메일을 바꾸면 그 뒤에 추가한 저장소부터
  적용되고, aline.team에 이미 있는 저장소는 처음 이메일로 그대로 집계됩니다.
- 같은 저장소를 aline.team 웹에서도 연동했다면 GitFolio의 커밋이 그 저장소로 합쳐집니다.

### 데이터 정책

**로그인한 뒤, 내가 고른 저장소에서, 내가 작성하고 push한 커밋의 메타데이터만** 수집합니다.
"내 커밋"은 작업 이메일(`gitfolio email`) 또는 저장소의 git 이메일로 작성한 커밋입니다.

| aline.team으로 보내는 것 | 수집하지 않는 것 |
|---|---|
| 저장소의 git 서비스(GitHub, GitLab 등)와 `소유자/저장소` | 소스 코드·파일 내용 |
| 대표 작업 이메일 | 내 컴퓨터의 폴더 위치(저장소 밖 경로) |
| 커밋 해시, 브랜치 이름, 시점(시간대 포함) | 원격 URL·호스트·인증 정보(git, `gh` 등) |
| 커밋 메시지 (이 컴퓨터에서 마스킹) | 다른 사람의 커밋 |
| 저장소 안 파일 경로, 추가·삭제 줄 수, 그 커밋에서 새로 만든 파일인지 | |
| AI 에이전트 참여 여부 (Claude Code, Codex, Cursor, Copilot 등) | |
| 내가 만든 파일을 다른 사람이 처음 고친 시각 (시각만, 누가 고쳤는지는 보내지 않음) | |

- **마스킹:** 저장·전송 전에 커밋 메시지 속 토큰·키, URL, 이메일, IP, 티켓 번호, `gitfolio config mask add`로 등록한
  금지어를 가립니다. 파일 경로·브랜치 이름·저장소 이름은 그대로 둡니다.
- **패키지 매니저 파일**(`package.json`, `pom.xml` 등)은 파일별로 승인한 경우에만, 의존성 파악 용도로만,
  요청할 때만 읽습니다: 승인한 직후, `gitfolio deps scan` 실행 시, 켜 두었다면 scan·sync·예약 동기화 때(`gitfolio deps auto on`).
  push 때는 읽지 않습니다. 의존성은 아직 전송하지 않습니다.
- **이 컴퓨터에 저장되는 것:** 수집 데이터와 로그인 토큰은 사용자 설정 폴더
  (`~/Library/Application Support/gitfolio`, `~/.config/gitfolio`, `%AppData%\gitfolio`)에 본인만 읽을 수 있게 저장합니다.
  토큰은 화면·로그에 출력하지 않습니다.
- **전송과 보관:** HTTPS로만 aline.team(Alineteam Inc., Google Cloud, 미국)에 보냅니다. 가입일로부터 1년간
  보관되고, 계속 이용 중이면 자동 연장됩니다. 가입은 [이용약관](https://aline.team/terms)과
  [개인정보처리방침](https://aline.team/privacy) 동의로 간주됩니다.
- **직접 관리:** `gitfolio sync --dry-run`으로 보낼 내용을 그대로 미리 보고, `gitfolio config autosync off`로 push 직후
  전송을 끄고, `gitfolio remove <경로> --purge`로 이 컴퓨터와 aline.team의 저장소 데이터를 지울 수 있습니다.

#### 직접 확인하기

GitFolio의 말을 믿지 않아도 직접 확인할 수 있습니다.

- **무엇을 읽는지:** GitFolio는 저장소를 `git` 명령으로만 읽고, 파일 내용은 `git cat-file`로만 읽습니다.
  GitFolio가 실행하는 git 명령은 git이 직접 기록합니다(`GIT_TRACE`, 기본 켜짐, 최대 1MB, 오래된 실행부터 삭제):
  ```sh
  gitfolio history               # 실행마다 gitfolio 명령과 그때 실행한 git 명령
  gitfolio history --all | grep cat-file    # 승인한 패키지 매니저 파일만 나와야 합니다
  ```
  `gitfolio config git-history off`로 기록을 끄고 지우며, `config git-history-size 4MB`로 상한을 바꿉니다.
  직접 따로 기록하려면:
  ```sh
  GIT_TRACE=$PWD/git-trace.txt gitfolio deps scan --all
  grep cat-file git-trace.txt
  ```
  Windows PowerShell: `$env:GIT_TRACE="$PWD\git-trace.txt"; gitfolio deps scan --all; Select-String cat-file git-trace.txt`
  뒤 `Remove-Item Env:GIT_TRACE`. GitFolio가 여는 파일 전체는 `strace -f -e trace=openat`(Linux),
  `sudo fs_usage -w -f filesys gitfolio`(macOS), Process Monitor(Windows)로 볼 수 있습니다.
- **무엇을 보내는지:** `gitfolio sync --dry-run`이 보낼 요청을 그대로 출력합니다.
- **무엇을 설치했는지:** v0.1.0 이후 릴리스에는 서명된 빌드 증명이 붙습니다.
  `gh attestation verify gitfolio_darwin_arm64.tar.gz -R Alineteam-Inc/GitFolio`로 그 파일이 이 저장소의 릴리스
  워크플로에서 태그된 공개 소스로 빌드됐음을 확인할 수 있습니다. 그 소스는 macOS·Linux·Windows CI를 통과했고,
  승인하지 않은 파일을 읽으면 실패하는 테스트도 거쳤습니다.

### 명령

```sh
gitfolio init                    # 데이터 정책, aline.team 가입·로그인, 저장소 찾기·선택
gitfolio add ~/code/my-project   # 저장소 등록, 커밋 수집, git 훅 설치
gitfolio list                    # 등록 저장소, 커밋 수, 훅 상태
gitfolio sync --dry-run          # aline.team에 보낼 내용을 그대로 미리 보기
gitfolio sync                    # aline.team에 없는 것만 전송 (push하면 자동 전송)
gitfolio config mask add 고객사A  # 커밋 메시지 속 고객사·사내 프로젝트명을 가림
gitfolio remove ~/code/my-project --purge   # 등록 해제, 기존 훅 복원, 이 컴퓨터와 aline.team의 데이터 삭제
gitfolio whoami | logout         # 이 기기의 로그인 계정 확인 / 로그아웃
gitfolio email add me@work.com   # 다른 작업 이메일 추가 (email primary <이메일>: aline.team에 보낼 대표 이메일)
gitfolio schedule 09:00          # 훅이 놓친 push를 매일 보완하는 예약 동기화 (선택, 해제: schedule off)
gitfolio deps scan --all         # 승인한 패키지 매니저 파일을 다시 읽어 의존성 갱신
gitfolio history                 # gitfolio가 실행한 git 명령 (git이 직접 남긴 기록)
gitfolio help                    # 전체 명령
```

### 라이선스

[MIT](LICENSE) © 2026 Alineteam Inc.
