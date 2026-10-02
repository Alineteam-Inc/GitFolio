# GitFolio

**Your git history, turned into a verified developer portfolio — without sharing a line of code.**

GitFolio by [Alineteam Inc.](https://aline.team) (macOS, Linux, Windows) sends metadata of the commits
you authored and pushed to [aline.team](https://aline.team), which turns it into your developer profile.
Latest: [v0.2.0](https://github.com/Alineteam-Inc/GitFolio/releases/latest) · [한국어](#한국어)

## Install

Requires `git`. The install scripts verify the SHA-256 checksum before installing.

**macOS**

```sh
brew install Alineteam-Inc/tap/gitfolio
```

or `curl -fsSL https://raw.githubusercontent.com/Alineteam-Inc/GitFolio/main/install.sh | sh`

**Linux**

```sh
curl -fsSL https://raw.githubusercontent.com/Alineteam-Inc/GitFolio/main/install.sh | sh
```

or, with Homebrew, `brew install Alineteam-Inc/tap/gitfolio`

**Windows** (PowerShell)

```powershell
irm https://raw.githubusercontent.com/Alineteam-Inc/GitFolio/main/install.ps1 | iex
```

## Get started

```sh
gitfolio init
```

Shows the data policy, signs you up or logs you in with an email code, and lets you choose the
repositories. After that every `git push` is sent automatically. Your profile is at
[aline.team/profile](https://aline.team/profile).

## Nothing is collected without your permission

- **Only after you log in, only from repositories you chose, only your own pushed commits.**
- **Only under emails you verified:** aline.team takes your commits under an email verified with a code
  sent to it (`gitfolio init`, `gitfolio email verify`), or a GitHub or GitLab noreply address.
- **Sent:** repository (`owner/repo`), commit hash, branch, time, message (masked on your computer),
  file paths with lines added/deleted, AI agent use.
- **Never collected:** source code or file contents, where repositories are on your computer,
  credentials, other people's commits.
- **Package manager files** (`package.json`, …) are read only if you approve each file, and only when
  you ask (`gitfolio deps scan`, or `gitfolio deps auto on`). Pushes never read them.
- **Check it yourself:** `gitfolio sync --dry-run` shows what would be sent, `gitfolio history`
  shows every git command GitFolio ran (recorded by git itself), and
  `gh attestation verify <archive> -R Alineteam-Inc/GitFolio` proves a release was built from this source.
- HTTPS only, to Alineteam Inc. (Google Cloud, United States). [Terms](https://aline.team/terms) ·
  [Privacy Policy](https://aline.team/privacy)

## Commands

```sh
gitfolio init                    # setup: data policy, sign-up/login, choose repositories
gitfolio add | remove <path>     # register a repository / unregister (--purge: delete its data here and on aline.team, web link included)
gitfolio list                    # registered repositories and their status
gitfolio sync [--dry-run]        # send what aline.team does not have yet (--dry-run: just show it)
gitfolio config mask add <word>  # hide a customer or project name in commit messages
gitfolio email add <email>       # another work email of yours (verify it with the emailed code to merge
                                 # repositories you also linked on aline.team)
gitfolio schedule 09:00          # optional daily sync (off: schedule off)
gitfolio deps [on|off|scan]      # dependency detection: per-file approval, read on request
gitfolio history                 # git commands GitFolio ran
gitfolio help                    # everything else
```

## License and name

The code is [MIT](LICENSE): use it, change it, fork it. The **name "GitFolio", "aline.team" and their
logos are not part of the license** — a modified or forked version must use its own name and logo and
must not suggest that Alineteam made or endorses it. Using the aline.team API is subject to the
[aline.team Terms of Service](https://aline.team/terms).

© 2026 Alineteam Inc. · Design and policy details: [docs/DESIGN.md](docs/DESIGN.md)

---

## 한국어

GitFolio는 내가 작성하고 push한 커밋의 메타데이터를 [aline.team](https://aline.team)으로 보내 개발 프로필을
만들어 주는 CLI입니다 (Alineteam Inc., macOS·Linux·Windows). 코드는 한 줄도 보내지 않습니다.

### 설치

`git`이 필요합니다. 설치 스크립트는 SHA-256을 검증한 뒤 설치합니다.

**macOS**

```sh
brew install Alineteam-Inc/tap/gitfolio
```

또는 `curl -fsSL https://raw.githubusercontent.com/Alineteam-Inc/GitFolio/main/install.sh | sh`

**Linux**

```sh
curl -fsSL https://raw.githubusercontent.com/Alineteam-Inc/GitFolio/main/install.sh | sh
```

또는 Homebrew가 있으면 `brew install Alineteam-Inc/tap/gitfolio`

**Windows** (PowerShell)

```powershell
irm https://raw.githubusercontent.com/Alineteam-Inc/GitFolio/main/install.ps1 | iex
```

### 시작하기

```sh
gitfolio init
```

데이터 정책을 보여 주고, 이메일 코드로 가입·로그인한 뒤, 수집할 저장소를 고릅니다. 이후 `git push`할 때마다
자동으로 전송됩니다. 프로필은 [aline.team/profile](https://aline.team/profile)에서 봅니다.

### 허락 없이 수집하지 않습니다

- **로그인한 뒤, 내가 고른 저장소에서, 내가 push한 내 커밋만** 수집합니다.
- **인증한 이메일로만:** aline.team은 메일로 받은 코드로 인증한 이메일(`gitfolio init`, `gitfolio email verify`)이나
  GitHub·GitLab noreply 주소로 쓴 커밋만 받습니다.
- **보내는 것:** 저장소(`소유자/저장소`), 커밋 해시·브랜치·시각, 커밋 메시지(이 컴퓨터에서 마스킹),
  파일 경로와 추가·삭제 줄 수, AI 에이전트 사용 여부
- **수집하지 않는 것:** 소스 코드·파일 내용, 내 컴퓨터의 저장소 위치, 인증 정보, 다른 사람의 커밋
- **패키지 매니저 파일**(`package.json` 등)은 파일별로 승인한 것만, 요청할 때만 읽습니다
  (`gitfolio deps scan` 또는 `gitfolio deps auto on`). push 때는 읽지 않습니다.
- **직접 확인:** `gitfolio sync --dry-run`으로 보낼 내용을, `gitfolio history`로 실행한 git 명령(git이 직접 기록)을 봅니다.
  `gh attestation verify <파일> -R Alineteam-Inc/GitFolio`로 릴리스가 이 소스로 빌드됐음을 확인합니다.
- HTTPS로만 Alineteam Inc.(Google Cloud, 미국)에 보냅니다. [이용약관](https://aline.team/terms) ·
  [개인정보처리방침](https://aline.team/privacy)

### 명령

```sh
gitfolio init                    # 설정: 데이터 정책, 가입·로그인, 저장소 선택
gitfolio add | remove <경로>     # 저장소 등록 / 해제 (--purge: 이 컴퓨터와 aline.team의 데이터·웹 연동 삭제)
gitfolio list                    # 등록 저장소와 상태
gitfolio sync [--dry-run]        # aline.team에 없는 것만 전송 (--dry-run: 보여 주기만)
gitfolio config mask add <단어>  # 커밋 메시지 속 고객사·프로젝트명 가리기
gitfolio email add <이메일>      # 다른 작업 이메일 추가 (메일로 온 코드로 인증하면 aline.team 웹에서도
                                 # 연동한 저장소와 하나로 합쳐짐)
gitfolio schedule 09:00          # 예약 동기화 (선택, 해제: schedule off)
gitfolio deps [on|off|scan]      # 의존성 분석: 파일별 승인, 요청할 때만 읽기
gitfolio history                 # GitFolio가 실행한 git 명령
gitfolio help                    # 그 밖의 명령
```

### 라이선스와 이름

코드는 [MIT](LICENSE)입니다. 자유롭게 쓰고, 고치고, 포크할 수 있습니다. 다만 **"GitFolio"·"aline.team" 이름과 로고는
라이선스에 포함되지 않습니다.** 고치거나 포크한 버전은 다른 이름과 로고를 써야 하고, Alineteam이 만들었거나 보증한 것처럼
보여서는 안 됩니다. aline.team API 사용은 [aline.team 이용약관](https://aline.team/terms)을 따릅니다.

© 2026 Alineteam Inc. · 설계·정책 상세: [docs/DESIGN.md](docs/DESIGN.md)
