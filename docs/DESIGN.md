# GitFolio 기획·설계 문서

> 상태: 초안 (2026-09-27) · 구현 전 합의 사항 기록 · 구현 순서는 [ROADMAP.md](ROADMAP.md)

---

## 0. 개요

### 0.1 목적

GitFolio는 개발자의 로컬 git 이력에서 **본인이 작성한 커밋의 메타데이터만** 수집·마스킹한 뒤, Alineteam Inc.의 서비스 **aline.team**으로 동기화해 개발 프로필·이력서 작성을 자동화하는 CLI 도구다. 소스 코드와 파일 내용은 수집하지 않는다.

### 0.2 대상 사용자

- 여러 저장소에 흩어진 본인 작업 이력을 정리하고 싶은 개발자
- 회사 저장소 작업 이력을 코드 노출 없이 경력으로 남기고 싶은 개발자
- AI 코딩 에이전트 활용 정도를 객관적으로 보여주고 싶은 개발자

### 0.3 제공 가치

| 가치 | 근거 |
|---|---|
| 자동 수집 | 최초 설정 후 push마다 자동 수집·전송, 누락분은 지정 시각에 보완 |
| 코드 비노출 | 메타데이터만 수집, 이 컴퓨터에서 마스킹 후 전송 |
| 기술 스택 파악 | 사용자가 승인한 패키지 매니저 파일에서 의존성 추출, 본인이 수정한 모듈 기준으로 귀속 |
| AI 활용 가시화 | 커밋별 AI 에이전트 사용 여부 분류 |

### 0.4 전체 흐름

```
[개발자 PC]                                                   [aline.team]

 git commit ─ post-commit 훅 ─▶ AI 에이전트 태그 기록 (네트워크 없음)

 git push ─── pre-push 훅 ────▶ (백그라운드) push 성공 확인
                                 → push된 커밋 수집·마스킹·AI 분류
                                 → 로컬 저장 ─── HTTPS + 기기 토큰 ───▶  분석·저장
                                                                         → 프로필 / 이력서
 예약 시각 / gitfolio sync /
 데스크톱 앱 버튼 ─────────────▶ 미전송·실패분 재전송 ─── HTTPS ─────▶
```

---

## 1. 설계 기조

| # | 원칙 | 의미 |
|---|---|---|
| 1 | **최소 수집** | 커밋 해시, 작성자 이메일, 커밋 메시지, 브랜치 이름, 시점, 저장소 안 파일 경로(git diff·log 표기), 추가·삭제 줄 수, AI 기여 여부, 저장소 namespace(`소유자/저장소`)만 수집. 파일 내용·저장소 밖 로컬 경로(내 컴퓨터의 폴더 위치)·원격 URL(호스트·인증 정보)은 수집하지 않음 |
| 2 | **파일 읽기는 승인 후에만** | 저장소 파일 내용을 읽는 것은 사용자가 **파일별로 승인한 패키지 매니저 파일**에 한하며, **의존성 파악 용도로만** 사용 |
| 3 | **저장·전송 전 마스킹** | 커밋 메시지의 민감한 부분(토큰·URL·이메일·IP·티켓 번호·금지어)은 원문을 로컬에도 서버에도 저장하지 않음. 수집 즉시 이 컴퓨터에서 마스킹. 파일 경로·저장소 이름은 마스킹하지 않음 (4장) |
| 4 | **push 시 자동 전송** | push에 성공한 커밋은 push 직후 백그라운드로 전송(기본값). 기기가 꺼져 있으면 실행되지 않는 예약 동기화에만 의존하지 않기 위함. 예약·수동 동기화는 누락·실패분 보완. 커밋·scan 자체는 네트워크를 쓰지 않음 |
| 5 | **투명성** | 전송 내용은 언제든 미리보기(`sync --dry-run`) 가능, 서버 데이터 삭제 요청 가능 |
| 6 | **개발 흐름 방해 금지** | 훅은 커밋·push를 절대 막지 않고 지연시키지 않음. 전송은 백그라운드에서만. 네트워크 장애·gitfolio 삭제·고장 시에도 git은 정상 동작 |
| 7 | **멱등성** | scan·sync를 몇 번 실행해도 결과가 같음. 레코드 ID 기준 중복 제거 |
| 8 | **저장소별 opt-in** | 사용자가 선택한 저장소만 수집. 전역 훅(`core.hooksPath`) 사용 안 함 |
| 9 | **사실 기반 AI 분류** | 확인된 신호만 사용, 추측 없음. 결과는 "확인된 AI 사용의 최소치" |
| 10 | **단일 바이너리** | 순수 Go(`CGO_ENABLED=0`), 실행 시 외부 의존은 `git` 하나 |

> 참고: git 이력 조회(`git log`), 추적 파일 **이름** 목록 조회(`git ls-tree`), 저장소 탐색 시 **폴더 이름** 확인은 파일 내용을 읽지 않으므로 원칙 2의 승인 대상이 아니다. 단, 모두 원칙 1의 범위에서만 사용한다.

## 2. 기술 결정

| 항목 | 결정 | 이유 |
|---|---|---|
| 언어 | Go | 크로스 컴파일, 단일 바이너리 |
| 라이선스 | MIT (Alineteam Inc.). 파생판 허용 | 통제는 라이선스가 아니라 이름·로고("GitFolio"·"aline.team", 파생판은 다른 이름·로고)와 aline.team API 이용약관으로 한다 (2026-10-02 사용자 결정, GPL-3.0·PolyForm Shield 검토 후 MIT 유지) |
| git 연동 | `git` 명령 실행 후 출력 파싱 | 라이브러리(go-git) 불필요, 사용자 환경의 git 설정(mailmap, includeIf) 그대로 반영 |
| CLI 파싱 | 표준 라이브러리 `flag` | 서브커맨드 수가 적어 프레임워크 불필요 |
| 로컬 저장 | JSON (설정), JSON Lines (커밋) | 표준 라이브러리로 처리, 사람이 읽을 수 있음 |
| 서버 통신 | 표준 `net/http`, HTTPS 전용 | |
| 예약 실행 | macOS `launchd`, Linux `systemd --user` 타이머 (없으면 `crontab`), Windows 작업 스케줄러(`schtasks`) | OS 기본 스케줄러 사용, 상주 프로세스 없음 |
| 화면 언어 | 영어 기본, 한국어·일본어 지원 (7.1). 그 밖의 언어(중국어 등)는 영어, 추가 계획 없음 (2026-10-01 사용자 결정) | 공개 배포 대상 |
| 배포 | GitHub Releases + Homebrew tap(macOS·Linux) + `install.sh`(macOS·Linux) + `install.ps1`(Windows) | GoReleaser로 자동화. Windows는 zip |
| 업데이트 확인 | 릴리스 빌드가 터미널에서 실행될 때 하루 한 번 `github.com/…/releases/latest` 리다이렉트로 최신 태그 확인 → 더 새 버전이면 "지금 업데이트할까요? [Y/n]" → 설치 방법대로 실행: Homebrew cask(`Caskroom` 경로)는 `brew update && brew upgrade --cask gitfolio`, Windows는 `install.ps1`, 그 밖은 `install.sh`(같은 폴더·그 버전). 성공하면 명령을 멈추고 다시 실행하게 한다. 훅·예약 실행·출력 리디렉션·소스 빌드에서는 확인하지 않음 (2026-10-01 사용자 요청) | API 호출 한도 없음, 설치 스크립트의 SHA-256 검증 재사용. 설치 스크립트는 실행 중인 바이너리를 덮어쓰지 않고 바꿔치기(이름 변경) |
| 대상 OS | **macOS·Linux·Windows** × amd64/arm64 (2026-09-30 사용자 결정: 세 OS 모두 지원·테스트) | CI(`.github/workflows/ci.yml`)가 push마다 세 OS에서 vet·test·설치 스크립트를 실행하고, 릴리스는 세 OS 통과 후에만 진행 |

## 3. 수집 규칙

### 3.1 수집 항목

| 항목 | git 출처 | 저장 형태 |
|---|---|---|
| 커밋 해시 | `%H` | 원본 그대로 저장·전송. git 서비스에서 커밋 실재를 검증하는 근거 (`hash`) |
| 작성자 이메일 | `%aE` (mailmap 반영) | 본인 식별에 쓴 이메일 (`authorEmail`) |
| 작성 형태 | `%aN`, `%aE`, 메시지 트레일러·문구 (5장) | 마스킹 **전** 원문으로 판정. `creationType`(`HUMAN` / `HUMAN_CO_AI` / `AI_CO_HUMAN`), `aiAgents`(예: `["claude-code"]`) |
| 저장소 namespace | `git remote get-url origin` (없으면 첫 번째 원격) | 원격 URL에서 `소유자/저장소`만 추출 (`namespace`, 예: `Alineteam-Inc/GitFolio`). 호스트·인증 정보(`https://user:token@…`)·포트·`.git`은 버림. aline.team 서버가 이 값으로 git 서비스 API를 조회해 저장소를 확정하고 커밋 실재를 검증 |
| git 서비스 | 원격 URL의 호스트 | `provider`: `GITHUB`, `GITLAB`, `BITBUCKET`, `DEVOPS`(Azure DevOps: `dev.azure.com`, `*.visualstudio.com`), 그 외(사내 서버 포함)는 `OTHER`. 호스트 자체는 전송하지 않음. Azure DevOps의 `namespace`는 `조직/프로젝트/저장소` (`_git`·`v3`·`DefaultCollection` 제거, 옛 `조직.visualstudio.com`은 호스트의 조직명을 앞에 붙임) — aline.team이 이것으로 `https://dev.azure.com/조직/프로젝트/_git/저장소`를 만듦. **원칙: `provider`+`namespace`로 서버가 만드는 주소는 git 서비스 웹 UI의 공유 주소와 같아야 한다** (GitHub `https://github.com/소유자/저장소`, GitLab `https://gitlab.com/그룹/하위그룹/프로젝트`, Bitbucket `https://bitbucket.org/워크스페이스/저장소`, Azure DevOps `https://dev.azure.com/조직/프로젝트/_git/저장소`, 경로 조각마다 퍼센트 인코딩). 원격 URL 형태별 기대 주소는 `scan_test.go` `TestRemoteGivesWebURL` |
| 시점 | `%aI` (author date) | ISO 8601, 타임존 포함 |
| 브랜치 | 원격 추적 브랜치 (`refs/remotes/…`) | 커밋이 있는 원격 브랜치 이름 하나 (`branch`, 원격 이름 제외). 기본 브랜치(`origin/HEAD`)에 들어간 커밋은 그 이름, 아니면 push된 브랜치. 웹 GitHub 연동 결과와 합쳐지는 기준. **마스킹하지 않음** (사용자 결정 2026-09-29: 웹 연동 결과와 같은 문서로 합쳐지도록 원래 이름 그대로. 금지어도 적용 안 함) |
| 커밋 메시지 | `%B` | 마스킹 후 저장 |
| 파일 경로 | `--numstat` | **저장소 루트 기준 상대 경로, git diff·log 표기 그대로** (예: `cmd/gitfolio/git.go`) — 사용자 결정 2026-09-30, 서버 권고(폴더가 달라도 이름이 같은 파일이 합쳐지지 않게). 이름 변경 시 변경 후 경로. 저장소 밖 로컬 경로는 보내지 않음. 마스킹하지 않음 (4장). 파일 이름만 저장하던 이전 형식은 다음 scan에서 한 번 다시 수집 (`Repo.Format`) |
| 파일 유형·언어 | — | CLI는 판정하지 않음. aline.team 서버가 파일 경로로 판단 |
| 추가·삭제 줄 수 | `--numstat` | 바이너리 파일(`-`)은 0 |

### 3.2 본인 식별

- **author 기준.** committer는 rebase·squash merge 시 바뀌므로 사용하지 않음
- **기본 이메일**: 각 저장소에서 유효한 `git config user.email` (전역·저장소별·`includeIf` 등 git이 해석한 값)
- **작업 이메일** (2026-09-30 서버 담당자 결정·사용자 확정): `gitfolio email add|rm|primary`로 관리하는 로컬 목록. 본인 커밋 = 등록한 작업 이메일 전체 + 저장소별 `git config user.email`. 인증 절차 없음
- **대표 이메일**: 목록의 첫 번째(★). 기본값은 최초 등록 이메일 — `init`이 git 이메일을 자동 등록하고, `init`을 거치지 않으면 처음 `add`한 저장소의 git 이메일. aline.team에는 본인 커밋 전부를 대표 이메일 하나로 보낸다. 로컬에는 실제 작성자 이메일을 그대로 둔다. 대표 이메일을 바꿔도 서버의 기존 저장소는 처음 이메일로 계속 집계되고, 새 저장소부터 새 대표 이메일을 쓴다 (서버 결정 2026-10-01, 이력 이전 없음)
- 본인 커밋 식별에만 사용하며, 계정 생성·로그인에는 쓰지 않음
- GitHub noreply 이메일은 본인 식별에 사용하지 않음
- 비교: 소문자 정규화 후 완전 일치

### 3.3 제외 대상

- merge 커밋 (`--no-merges`)
- 본인이 author도 아니고, AI 에이전트가 author인 커밋의 `Co-authored-by`에도 본인이 없는 커밋

### 3.4 수집 범위

| 시점 | 대상 |
|---|---|
| 저장소 등록 시 (`init` 또는 `add`, 최초 1회) | 원격에 올라간 커밋 전체 (`--remotes`). 원격 추적 브랜치가 없으면 `HEAD`를 읽어 이 컴퓨터에만 두고 **보내지 않음** — 브랜치가 없는 커밋은 push된 적이 없는 것 (첫 push 전이거나 첫 push 실패, 2026-10-02). 첫 push가 성공하면 원격 추적 브랜치 기준으로 다시 수집 |
| push 직후 (`pre-push` 훅 → 백그라운드) | push가 끝난 뒤 원격 추적 브랜치에 올라간 커밋. 거절된 push의 커밋은 원격 추적 브랜치에 없으므로 수집되지 않음 (6.2) |
| 동기화 직전 (`sync`) | 등록된 모든 저장소 증분 scan (다른 경로로 push된 커밋 보완) |

push된 커밋마다 수집하는 내용: 커밋 메시지, 시점, 변경 파일 경로, 파일별 추가·삭제 줄 수 (3.1)

→ 로컬에만 있다가 amend·rebase로 사라지는 커밋은 수집하지 않는다.

### 3.5 의존성 (승인 기반)

프레임워크·주요 라이브러리 파악을 위해 **사용자가 파일별로 승인한** 패키지 매니저 파일에서 의존성 이름만 추출한다. 루트·하위 경로 구분 없이 모든 파일이 승인 대상이다.

**2단계 동의**

1. **기능 동의** (`init` 또는 `gitfolio deps on`): 의존성 분석 기능 사용 여부. 기본값 **사용 안 함**
2. **파일별 승인**: 발견된 매니저 파일 목록을 보여주고 사용자가 고른 파일만 읽음. 여러 저장소를 한꺼번에 다룰 때(`init`, `deps on`)는 **한 목록으로 모아 한 번만** 묻는다 — 저장소별로 묶고 번호는 이어서 매김 (2026-10-01 사용자 결정: 저장소마다 묻는 것은 반복적)

```
Package manager files. Selected files are read only to detect dependencies:
my-api
    1  new       build.gradle
    2  new       api/build.gradle
my-web
    3  new       pom.xml
? Allow reading which files? [all / none / 1,3,5-7 / Enter = keep as is] >
```

| 규칙 | 내용 |
|---|---|
| 후보 찾기 | `git ls-tree -r --name-only HEAD`의 파일 **이름**만 보고 지원 목록과 일치하는 것 선택. 이 단계에서 내용은 읽지 않음 |
| 후보 제외 | `node_modules/`, `vendor/`, `third_party/`, `examples/`, `testdata/`, `fixtures/` 하위 (남의 코드·샘플) |
| 개수 상한 | 저장소당 후보 200개. 초과분은 표시하지 않고 경고 |
| 읽는 위치 | 승인된 파일의 `HEAD` 커밋 버전만 (`git cat-file -p HEAD:<경로>`). 작업 중·미추적 파일은 읽지 않음 |
| 사용 범위 | 의존성 이름·버전 추출에만 사용. 파일 원문은 메모리에서 파싱 후 즉시 버림 |
| 저장 내용 | 모듈별 생태계, 의존성 이름, 선언된 버전. **파일 원문과 경로는 저장·전송하지 않음** (모듈은 `m1`, `m2` 같은 불투명 ID) |
| 제외 의존성 | lock 파일(간접 의존성), 로컬 경로·git URL·사설 저장소 URL로 선언된 의존성 |
| 마스킹 | 의존성 이름은 마스킹하지 않음 (커밋 메시지만 마스킹, 4장) |
| 결정 기록 | 승인·거절은 로컬 `repos.json`에만 기록, 서버로 전송하지 않음. 거절한 파일은 다시 묻지 않음 |
| 새 파일 | 훅 실행 중(비대화형) 새로 발견된 파일은 읽지 않고 **확인 대기**. 다음 대화형 명령 실행 시 알림, `gitfolio deps review`로 처리 |
| 읽는 시점 | 요청할 때만: 파일을 승인한 직후(`init`·`deps on`·`deps review`), `gitfolio deps scan [경로] [--all]`, 그리고 `deps auto on`일 때 `scan`·`sync`·예약 동기화(예약도 `sync`). `deps auto`는 하나의 옵션으로 셋을 함께 켜고 끄며 기본 off. push 직후 수집과 `add`는 읽지 않고, 승인 경로로 모듈 ID만 정함(내용 미열람) (2026-10-01 사용자 결정) |
| 철회 | `deps off` → 로컬 의존성 데이터와 승인 기록 삭제, 다음 sync 때 서버 삭제 요청. `deps review`에서 개별 파일 승인 취소 가능 |

**1차 지원 파일**

| 생태계 | 파일 | 파싱 |
|---|---|---|
| Node | `package.json` | JSON (`dependencies`, `devDependencies`) |
| Go | `go.mod` | `require` 줄 |
| Python | `requirements.txt`, `pyproject.toml` | 줄 단위 |
| Java/Kotlin | `pom.xml`, `build.gradle`, `build.gradle.kts`, `libs.versions.toml` (Gradle 버전 카탈로그) | XML / 의존성 선언 줄 / `[libraries]` 구역 |
| Rust | `Cargo.toml` | `[dependencies]` 구역 |

그 외(`Gemfile`, `composer.json`, `*.csproj`, `pubspec.yaml`, `Package.swift` 등)는 필요 시 추가.

**모듈 귀속 (본인 사용 여부 추정)**

"저장소에 의존성이 있다 ≠ 본인이 그 라이브러리를 다뤘다"를 보완하기 위해, 본인 커밋이 **어느 모듈을 수정했는지**와 결합한다.

1. 승인된 매니저 파일이 있는 디렉터리 = 모듈. 상위 모듈은 하위 모듈에 속하지 않는 나머지를 담당
2. scan 시 각 변경 파일의 경로로 **가장 가까운 상위 모듈**을 찾아 로컬 커밋 레코드에 모듈 ID를 기록 (모듈 ID는 로컬 전용). 모듈 ID는 디렉터리 경로의 SHA-256 앞 8자리
3. 전송(export) 시 **본인 커밋이 수정한 모듈의 의존성만** 저장소별 목록으로 보낸다. 모듈 ID 자체는 로컬 전용이며 전송하지 않는다
4. aline.team 서버는 이 의존성 목록으로 사용자의 프레임워크·미들웨어·라이브러리 스택을 판단한다

| 지표 | 의미 |
|---|---|
| `commits` | 해당 의존성을 선언한 모듈을 수정한 본인 커밋 수 |
| `lines` | 그 커밋들의 추가+삭제 줄 수 |
| `ext_match` | 수정한 파일 확장자가 해당 생태계와 일치하는 비율 (예: npm 의존성 모듈에서 `.ts`·`.js` 수정) |

→ 모노레포에서 백엔드만 작업한 사람에게 프런트엔드 라이브러리가 붙지 않는다.

## 4. 마스킹

수집 시점에 **커밋 메시지에만** 적용해 민감한 부분을 가린다 (사용자 결정 2026-09-29). 파일 경로·저장소 이름·브랜치 이름·의존성 이름은 원래 이름 그대로 둔다 — 저장소 이름은 어차피 `namespace`(`소유자/저장소`)로 전송되고, 브랜치 이름은 웹 연동 결과와 합쳐지도록 원래 이름이 필요하다 (3.1).

| 대상 | 탐지 | 치환 |
|---|---|---|
| URL | 정규식 | `[URL]` |
| 이메일 | 정규식 | `[EMAIL]` |
| IP 주소 | 정규식 | `[IP]` |
| 티켓 번호 | `ABC-123`, `#123` | `[TICKET]` |
| 토큰·키 | 알려진 접두사 (`ghp_`, `github_pat_`, `sk-`, `AKIA`, `xox*-`, JWT, 개인 키 헤더, aline.team 토큰 `aln_cli_` 등) | `[SECRET]` |
| 고객사명·내부 프로젝트명 | 사용자 등록 금지어 (대소문자 무시) | `[REDACTED]` |

- 적용 순서: AI 판정(원문) → 토큰·키 → URL → 이메일 → IP → 티켓 번호 → 금지어. 결과를 다시 마스킹해도 달라지지 않음
- 티켓 번호 패턴에서 `UTF-8`, `SHA-256`, `ISO-8601` 같은 표준 명칭은 제외 (허용 목록)
- 커밋 메시지에만 적용. 금지어(`config mask`)도 커밋 메시지에만 적용됨. `namespace`·`authorEmail`은 검증·식별용이므로 마스킹하지 않음
- 금지어를 **추가**하면 로컬 데이터에 즉시 재적용. 이미 전송된 레코드는 다음 sync 때 재전송해 서버 데이터를 덮어씀
- 금지어를 **삭제**해도 이미 가려진 값은 복원되지 않는다. 필요하면 `scan --rebuild`로 git에서 다시 수집한다
- 규칙 기반이므로 누락·과잉 가능. `sync --dry-run`으로 전송 전 확인

## 5. AI 에이전트 분류

### 5.1 분류

| `creationType` | 조건 | 예 |
|---|---|---|
| `HUMAN` | author가 본인, AI 신호 없음 | 직접 작성한 커밋 |
| `HUMAN_CO_AI` | author가 본인, AI 신호 있음 (트레일러·문구·커밋 시점 환경변수) | Claude Code·Codex로 작업해 본인 이름으로 커밋 |
| `AI_CO_HUMAN` | author가 AI 에이전트, `Co-authored-by`에 본인 | Copilot coding agent, Cursor 클라우드 에이전트 |

- `aiAgents`: 확인된 에이전트 목록 (예: `["claude-code"]`)
- author가 에이전트이고 공동 작성자에 본인이 없으면 누구의 커밋인지 알 수 없으므로 수집하지 않는다. 그래서 `AI` 단독 값은 두지 않는다 (플러그인 등 근거가 생기면 추가)
- 클라우드 에이전트는 보통 사용자의 GitHub noreply 주소를 공동 작성자로 넣는다. 이 주소가 본인 식별 이메일에 없으면 `AI_CO_HUMAN` 커밋은 수집되지 않는다 (9장)

### 5.2 탐지 단계

| 단계 | 시점 | 신호 | 신뢰도 |
|---|---|---|---|
| ① 환경변수 | 커밋 시 (`post-commit` 훅) | 에이전트가 자식 프로세스에 설정하는 환경변수 | 높음 |
| ② 트레일러·문구 | scan 시 | `Co-authored-by` **이메일**, `Assisted-by:`, 메시지 표시 문구 | 중간 (생략·위조 가능) |
| ③ 봇 작성자 | scan 시 | author 이메일이 에이전트·봇 계정 | 높음 (클라우드 에이전트) |

- 트레일러는 이름이 아닌 이메일로 비교
- 신호 목록은 코드 내 표 하나로 관리. 새 에이전트는 한 줄 추가

### 5.3 에이전트별 신호 (2026-09-27 조사)

| 에이전트 | ① 환경변수 | ② 트레일러 이메일 / 문구 | ③ 봇 작성자 |
|---|---|---|---|
| Claude Code | `CLAUDECODE=1`, `AI_AGENT=claude-code_*` | `noreply@anthropic.com`, `Generated with [Claude Code]` | — |
| Codex CLI | `CODEX_CI=1`, `CODEX_THREAD_ID` | `noreply@openai.com` | `codex@openai.com` (cloud) |
| Cursor | `CURSOR_AGENT` | `cursoragent@cursor.com`, `Made-with: Cursor` | `cursoragent@cursor.com` |
| Copilot | `COPILOT_AGENT=1` (VS Code), `COPILOT_CLI=1` | `copilot@github.com`, `…+Copilot@users.noreply.github.com` | `copilot-swe-agent[bot]` |
| Gemini CLI | `GEMINI_CLI=1` (훅 비활성화, 9장 참고) | — | — |
| Aider | — | `aider@aider.chat`, 작성자 이름 `(aider)` | — |
| Amp | `AGENT=amp` (미검증) | `amp@ampcode.com`, `Amp-Thread-ID:` | `amp@ampcode.com` |
| OpenCode | `OPENCODE=1` | — | `opencode-agent[bot]` |
| Cline / Roo Code | `CLINE_ACTIVE` / `ROO_ACTIVE` | — | — |
| Goose | `AGENT_SESSION_ID` | — | — |
| Warp | — | `agent@warp.dev`, `oz-agent@warp.dev` | `agent@warp.dev` |
| Devin / Jules / Kiro / Amazon Q | — | 각 봇 이메일 | 각 `[bot]` 계정 |
| 공통 | `AI_AGENT`, `AGENT` | `Assisted-by:` | `*[bot]@users.noreply.github.com` |

## 6. aline.team 연동

### 6.1 가입·로그인

> 서버와 합의한 계약: 비공개 서버 문서 (2026-09-29)

**aline.team 가입은 필수이며, 가입·로그인 전에는 GitFolio가 아무것도 수집하거나 동작하지 않는다.** 로그인 전에는 저장소를 읽는 명령(`add`, `scan`, `deps on|review`)이 "먼저 로그인" 안내와 함께 거부되고, 훅은 아무 일도 하지 않는다. 보기·정리·설정 명령(`list`, `export`, `remove`, `deps`, `deps off`, `config`, `whoami`, `logout`, `version`, 도움말)은 동작한다 — 로그아웃한 사용자도 서버 주소를 바꾸거나(`config api-url`) 저장소 등록 해제·의존성 삭제를 할 수 있어야 하므로. `init`은 1단계 로그인을 마쳐야 다음 단계로 진행한다.

계정은 사용자가 직접 입력하고 소유를 확인한 이메일로만 만든다. `git config user.email`은 계정 생성·로그인에 사용하지 않는다 (누구나 임의의 값으로 바꿀 수 있음). 사용자가 토큰을 직접 복사·입력하는 일은 없다.

1. **고지**: 계정이 없으면 새로 가입되며, 가입하면 이용약관(https://aline.team/terms)과 개인정보처리방침(https://aline.team/privacy, 국외 이전 내용 포함)에 동의한 것으로 간주함을 이메일 입력 전에 안내 (웹 가입과 같은 방식, 별도 동의 단계·기록 없음)
2. **이메일 입력**: 사용자가 직접 입력 (자동 채움 없음)
3. **이메일 소유 확인**: 서버가 해당 이메일로 6자리 코드 발송 → CLI에 입력. 틀리면 재입력
4. **가입 또는 로그인**: 미가입 이메일이면 서버가 가입 처리, 기존 계정이면 로그인 (가입 경로 ALINE·Google·GitHub·LinkedIn 무관). 웹에서 가입하고 이메일 인증을 안 한 계정은 거부되므로 웹 인증 먼저
5. **토큰 발급**: 이 기기 전용 불투명 토큰(`aln_cli_…`)을 받아 `credentials.json`(권한 0600)에 저장, 이후 요청에 `Authorization: Bearer <토큰>`으로 사용. 기기 이름은 OS·아키텍처만 전송

> ⚠️ git credential helper, `gh` 인증 정보 등 **다른 서비스의 인증 정보는 읽거나 전송하지 않는다.**

| 규칙 | 내용 |
|---|---|
| 토큰 취급 | 사용자에게 표시하지 않음. 로그·에러 메시지·`--dry-run` 출력에도 표시하지 않음. 커밋 메시지에 섞이면 마스킹(`aln_cli_` 패턴) |
| 수명 | **활동 기반 슬라이딩 만료**: 인증된 호출(push 직후 전송·sync)이 있으면 연장, **일정 기간 비활동 시 서버가 폐기**. 평소 push하는 사용자는 로그인이 유지됨 |
| 만료·폐기 | 서버가 `A001`로 거부하면 로컬 토큰을 삭제하고 `gitfolio login` 재로그인 안내. **자동 재발급 없음** |
| 기기 분실·교체 | aline.team 웹의 CLI 토큰 목록에서 해당 토큰 폐기. 서버 API(`GET [internal]`, `DELETE …/{id}`)는 있고 **웹 화면은 추후** (2026-09-29 결정).  |
| 로그아웃 | `logout`: 서버에 토큰 폐기 요청(실패해도 진행) + 로컬 토큰 삭제 |

- **기기 키(ed25519) 방식은 채택하지 않음** (서버 결정, 2026-09-29): 개인 키와 토큰이 같은 `credentials.json`에 있어 보안 이득이 작다는 판단. 토큰 만료 시 재로그인

#### 6.1.2 작업 이메일 추가 인증 — ❌ 제외 (인증 없는 로컬 작업 이메일 목록으로 대체, 3.2)

> **재도입 결정 (2026-10-02, 사용자)**: 웹 git 연동 저장소와 CLI 저장소가 둘로 남는 중복을 없애려고 작업 이메일 추가(`gitfolio email add`)에 이메일 코드 인증을 붙인다. 병합 조건 = 같은 계정 + `namespace` 일치(대소문자 무시) + 웹 저장소 작성자 이메일이 그 사용자의 **인증된 이메일**(계정 이메일 + 인증한 작업 이메일, 주·보조 무관). 저장소별 `git config user.email`은 인증 없이 본인 커밋 판별에만 쓰고 병합 근거로 쓰지 않는다.  서버 계약 확정 뒤 CLI 구현 (REMAINING)



본인 커밋 식별(3.2)의 기본 이메일 외에, 회사·개인 등 **다른 작업 이메일을 인증받아 추가**할 수 있다.

1. `gitfolio config email add <이메일>`
2. 서버가 해당 이메일로 일회용 코드 발송 → CLI에 입력
3. 인증되면 계정의 인증 이메일 목록에 추가되고, 이후 scan부터 해당 이메일의 커밋도 본인 커밋으로 수집
4. `gitfolio config email rm <이메일>`: 목록에서 제거. 이미 수집된 커밋은 유지하며, 삭제하려면 `--purge`

- 인증 이메일 목록은 계정 기준으로 서버에 저장되어 같은 계정의 모든 기기에 적용 (sync 때 로컬 캐시 갱신)
- 인증되지 않은 이메일은 추가되지 않음

### 6.2 동기화

| 방식 | 시점 | 역할 |
|---|---|---|
| **push 동기화 (기본)** | push 성공 직후 | push된 커밋을 즉시 전송 |
| 예약 동기화 (선택) | 사용자가 직접 켠 경우에만, 지정한 시각 (`gitfolio schedule 09:00`) | 최후의 수단. 훅을 거치지 않은 push(`--no-verify`, 다른 PC, 훅을 끄는 도구)의 누락분 보완. 기본값 꺼짐 |
| 수동 동기화 | `gitfolio sync` 또는 데스크톱 앱의 동기화 버튼 | 즉시 전체 동기화 |

**push 동기화 절차**

1. `pre-push` 훅이 `git push` 프로세스 ID(`$PPID`)를 넘겨 백그라운드 프로세스(`gitfolio hook push-wait`)를 띄우고 즉시 종료 → push는 지연 없이 진행
2. 백그라운드: `git push` 프로세스가 끝나길 기다림 (터미널을 닫아도 계속 진행). Windows는 프로세스 트리에서 가장 가까운 `git.exe`를 찾는다. 찾지 못하면(Git for Windows가 husky의 `#!/usr/bin/env sh` 스텁을 env로 실행하면 중간 프로세스가 끝나 부모 연결이 끊김) 훅 시점의 원격 추적 브랜치 지문을 넘기고, 그것이 바뀌어 멈출 때까지 기다린다 (2026-10-01)
3. 원격 추적 브랜치(`refs/remotes/…`)에 올라간 커밋을 scan → 수집·마스킹·AI 분류 → 전송. push가 실패했다면 원격 추적 브랜치가 그대로이므로 새로 수집되는 커밋이 없음
4. push 실패(거절 등) → 전송하지 않음. 다음 push에 다시 포함됨
5. 오프라인·서버 오류 → 미전송으로 남기고 다음 push·예약·수동 동기화 때 재전송
6. 백그라운드 대기 상한 10분. 초과 시 포기하고 다음 동기화에서 보완

- 로그인 전에는 훅이 아무것도 하지 않음 (수집·전송 모두 없음)
- `gitfolio config autosync off`로 push 동기화를 끌 수 있음 (예약·수동만 사용)

**`sync` 순서** (예약·수동): 등록 저장소 증분 scan → 미전송·변경 레코드 전송 → 삭제 요청 전송 → 결과 기록

| 항목 | 설계 |
|---|---|
| 전송 내용 | 마스킹된 레코드 (3.1 항목). 로컬 경로, 원격 URL(호스트·인증 정보), 매니저 파일 경로·원문은 전송하지 않음 |
| 레코드 ID | 저장소 + 커밋 해시. 여러 PC에서도 같은 커밋은 같은 ID |
| 재전송 | 금지어 추가로 재마스킹된 레코드는 같은 ID로 덮어씀 |
| 삭제 | `remove --purge`, `deps off`로 생긴 삭제 요청을 다음 sync 때 전송 |
| 실패 | 오프라인·서버 오류 시 미전송 상태 유지, 다음 sync에서 재시도 |
| 미리보기 | `sync --dry-run`: 전송될 JSON을 그대로 출력하고 전송하지 않음 |
| 통신 | HTTPS 전용, 인증서 검증 필수 |
| 로그인 전 | 동작하지 않음 (6.1) |

### 6.3 데이터 보관 지역과 국외 이전

- 서버 데이터는 **미국**(Google Cloud Platform)에 보관. CLI 문서·화면에는 리전을 적지 않는다 (2026-10-01 사용자 결정)
- **보관 기간**: 최초 가입일로부터 1년. 1년이 지난 시점에 계속 이용 중(동기화 호출이 지속적으로 발생)이면 **자동 연장**
- 이용약관: https://aline.team/terms, 개인정보처리방침: https://aline.team/privacy
- 국외 이전 내용은 개인정보처리방침에 공개되어 있고, **가입을 이용약관·개인정보처리방침 동의로 간주**한다 (서버 결정, 웹 가입과 동일). 별도 동의 화면·기록은 두지 않음
- CLI는 시작 화면의 데이터 정책(7.2)과 `login`의 가입 고지(6.1)에서 미국 전송 사실과 두 문서 주소를 알린다

### 6.4 데스크톱 앱 연계

- 데스크톱 앱은 UI의 동기화 버튼으로 수동 동기화를 실행한다
- 로직을 한 곳에 두기 위해 앱은 CLI를 호출하는 방식을 권장: `gitfolio sync --json`, `gitfolio list --json` (사람용 문구 대신 기계가 읽는 JSON 출력)
- 앱과 CLI는 같은 로컬 저장 폴더·기기 토큰을 공유

## 7. 명령어

```
gitfolio init [경로...]        최초 설정 (7.2): 데이터 정책 → aline.team 가입·로그인 → 저장소 찾기·선택
gitfolio login                 aline.team 가입·로그인 (이메일 확인 코드 → 토큰 자동 저장)
gitfolio logout                서버 토큰 폐기 요청 + 기기 토큰 삭제
gitfolio whoami                로그인 계정·인증 이메일·서버 주소
gitfolio add [경로]            저장소 1개 수동 등록 + 최초 수집 + 훅 설치
gitfolio remove [경로]         등록 해제 + 훅 복원 (--purge: 로컬 삭제 + 서버 삭제 요청)
gitfolio scan [경로]           증분 수집 (--all: 등록된 전체, --rebuild: 재수집). 네트워크 없음
gitfolio sync [--dry-run]      전체 증분 수집 후 미전송·변경분 전송 (--dry-run: 보낼 요청 그대로 출력)
gitfolio list                  등록 저장소, 커밋 수, 훅 상태, 의존성 파일, 마지막 수집
gitfolio export                로컬 데이터를 JSON으로 출력 (전송 형태와 같은 필드)
gitfolio deps [on|off]         의존성 분석 상태 보기·켜기·끄기 (off: 로컬 삭제. 서버 전송·삭제는 서버 2차)
gitfolio deps review [경로]    매니저 파일 승인·거절 변경, 확인 대기 처리
gitfolio deps scan [경로] [--all]  승인한 매니저 파일을 다시 읽어 의존성 갱신 (push·scan·sync는 읽지 않음, 3.5)
gitfolio config                현재 설정 출력
gitfolio config mask add|rm <금지어>   커밋 메시지에 적용
gitfolio config api-url <url>|default  서버 주소 (GITFOLIO_API_URL이 우선). 개발 전용: help에 없고, 릴리스 빌드는 거부·무시하고 항상 운영 주소 (2026-10-01 사용자 결정)
gitfolio config autosync on|off        push 직후 전송 켜기·끄기 (기본 on)
gitfolio config git-history on|off     git 명령 기록 켜기·끄기 (기본 on, off: 기록 삭제) (7.8)
gitfolio config git-history-size <크기> git 명령 기록 최대 크기 (기본 1MB, 16KB–100MB)
gitfolio history [--all]       gitfolio가 실행한 git 명령, 실행별 (기본 최근 20회) (7.8)
gitfolio email [add|rm|primary <이메일>]  작업 이메일 목록·대표 이메일(★, 첫 번째) 관리 (3.2)
gitfolio schedule [HH:MM|off]  예약 동기화 설정·해제, 인자 없으면 예약 시각·마지막 동기화 결과 (7.5)
gitfolio deps auto on|off      scan·sync·예약 동기화 때 승인한 매니저 파일도 읽기 (기본 off, push는 읽지 않음, 3.5)
gitfolio version | help
gitfolio hook post-commit|pre-push|push-wait   (내부용, 훅에서 호출)
```

미구현 (데스크톱 앱·편의 기능, 필요할 때): `list --json`·`sync --json`(앱용 출력), `list`의 저장소별 미전송 수, `export --format md`·`--since`·`-o 파일`

`[경로]` 생략 시 현재 디렉터리.

### 7.1 화면 언어

- 기본 **영어**. 기기 언어가 한국어·일본어이면 해당 언어로 표시
- 판별 순서
  1. `GITFOLIO_LANG` (직접 지정, 항상 우선)
  2. macOS: 시스템 설정 › 언어 및 지역의 선호 언어 중 GitFolio가 지원하는 첫 언어 (`defaults read -g AppleLanguages`, 실행당 1회). 터미널은 기기 언어와 무관하게 `LANG=en_US.UTF-8`인 경우가 많아 환경변수보다 우선
  3. `LC_ALL` → `LC_MESSAGES` → `LANG` 중 처음 설정된 값의 앞 두 글자 (`ko_KR.UTF-8` → `ko`). Linux는 이것이 기기 설정
  4. 그 외는 영어
- Windows는 환경변수가 보통 없으므로 Windows 지원 시 OS API(`GetUserDefaultUILanguage`) 판별 추가
- 문단: 이어서 출력되는 결과 줄은 한 문단으로 보고 첫 줄에만 접두어를 붙이며, 다음 줄은 글자 위치에 맞춰 들여쓴다. 빈 줄·안내문·질문·구역 제목이 오면 문단이 끝나고 다음 결과는 다시 접두어로 시작. 오류는 항상 새 문단 (사용자 결정 2026-09-30)
- 출력 모양: 결과·오류 줄은 `  GitFolio >> …`(오류는 stderr, `오류: …`), 질문(입력을 기다리는 줄)은 `  ? …` (사용자 결정 2026-09-30, 여러 줄 질문은 둘째 줄부터 질문 글자에 맞춰 들여씀), 안내문은 앞 여백(2칸)만, 대화형 명령 시작은 `===== GitFolio · 제목 =====`. `list` 표·`export`/`config` JSON·`help`는 접두어 없음 (복사·파이프용)
- 오류도 화면 언어로 표시 (`오류: 등록되지 않은 저장소입니다: ~/Code/demo`). 서버 오류는 응답의 `messageKo`·`messageEn`·`messageJa`, git이 출력한 오류와 훅 내부 오류는 원문 그대로
- 메시지는 Go 코드 안의 언어별 표로 관리 (외부 라이브러리 없음). 번역이 없는 메시지는 영어로 대체
- 번역 대상은 사람이 읽는 화면 문구만. JSON 키·훅 출력·에러 코드는 번역하지 않음
- 정책·동의 문구는 모든 언어에서 의미가 동일해야 함. 일본어는 원어민 검수 필요

### 7.2 `init` (최초 실행)

brew·`curl | sh` 설치 과정에서는 사용자 입력을 받을 수 없으므로(`curl | sh`는 stdin이 스크립트 자체), 설치 완료 메시지로 `gitfolio init` 실행을 안내한다.

설치 후 첫 실행에서 로그인과 함께 기본 설정을 한 번에 마치는 순서 (사용자 결정 2026-09-30):

| 단계 | 화면 구역 | 내용 |
|---|---|---|
| 0 | 시작 화면 | 로고 · 라이선스 · 데이터 정책 → Enter |
| 1 | `로그인` | **aline.team 가입·로그인 (필수)**: 이용약관·개인정보처리방침 고지 → 이메일 입력 → 확인 코드 → 토큰 저장. 완료하지 않으면 여기서 종료 |
| 2 | `저장소` | 저장소 모음 경로 → 작업 이메일(git 이메일 자동 등록 = 대표, "다른 작업 이메일이 있나요?" 추가) → 탐색 → 후보 표시 → **등록할 저장소 선택** (Enter = 선택 안 함) |
| 3 | `설정` | ① **의존성 분석** [y/N] (고지 후) ② **예약 동기화** [HH:MM / Enter = 안 함] — 정해진 시각의 자동 수집·전송. 예약하지 않으면 사용자가 `gitfolio sync`로 직접 실행 |
| 4 | | 설정 적용 (의존성을 새로 켜면 이미 등록된 저장소의 매니저 파일도 승인, 끄면 수집분 삭제. 예약은 7.5 방식으로 등록·해제) → 선택한 저장소 등록 (의존성 분석이 켜져 있으면 고른 저장소 전체의 매니저 파일을 한 번에 승인) |
| 5 | `동기화` | 등록 저장소가 있으면 `sync` (첫 실행은 선택한 저장소의 이력 전송). 실패해도 다음 push·sync 때 전송 |
| 6 | | 요약: 등록 저장소 수, push 직후 자동 전송(현재 값), 의존성 분석, 예약 동기화와 바꾸는 방법 |

- 가입·로그인을 가장 먼저 한다. 로그인 전에는 탐색·수집을 포함해 아무것도 하지 않는다
- 설정 질문의 기본값은 **현재 설정**이다: 다시 실행하면 로그인은 건너뛰고, 아직 등록되지 않은 저장소만 표시하며, 바꾼 설정만 바뀐다 (설정 마법사로 다시 쓸 수 있음)
- push 직후 자동 전송은 init에서 묻지 않는다 (기본 켜짐, `gitfolio config autosync on|off`). 사용자가 말한 "자동 스캔"은 훅이 아니라 예약에 따른 자동 수집으로, 예약 동기화 질문과 수동 `sync`로 다룬다 (2026-09-30)
- 어느 단계에서 중단해도 이미 완료된 설정은 유지

**0. 시작 화면**

```
    _     _      ___  _   _  _____  _____  _____     _     __  __
   / \   | |    |_ _|| \ | || ____||_   _|| ____|   / \   |  \/  |
  / _ \  | |     | | |  \| ||  _|    | |  |  _|    / _ \  | |\/| |
 / ___ \ | |___  | | | |\  || |___   | |  | |___  / ___ \ | |  | |
/_/   \_\|_____||___||_| \_||_____|  |_|  |_____|/_/   \_\|_|  |_|
 =================================================================
 :: GitFolio ::                                          (v0.1.0)
 MIT License - Copyright (c) 2026 Alineteam Inc.

 [Data Policy]
 - Source code is never collected.
 - Collected: commit hashes, author emails, commit messages
   (masked), branch names, timestamps, file paths in the
   repository, lines added/deleted, AI usage, and repository
   namespaces (owner/repo).
 - Files are read only with your approval, and only package
   manager files, only to detect dependencies.
 - Sensitive parts of commit messages (tokens, URLs, emails,
   ticket numbers, blocked words) are masked on this computer.
   Data is sent to aline.team (Alineteam Inc., United States)
   after each git push and when you sync. Kept for 1 year from
   sign-up, renewed automatically while you keep using GitFolio.
 - An aline.team account is required.
 - Privacy policy: https://aline.team/privacy

 Press Enter to continue, or Ctrl+C to quit.
```

한국어(`ko`) 정책 문구:

```
 [데이터 정책]
 - 소스 코드는 수집하지 않습니다.
 - 수집 항목: 커밋 해시, 작성자 이메일, 커밋 메시지(마스킹), 브랜치 이름,
   시점, 저장소 안 파일 경로, 추가·삭제 줄 수, AI 사용 여부,
   저장소 namespace(소유자/저장소)
 - 파일 읽기는 사용자가 승인한 패키지 매니저 파일에 한하며,
   의존성 파악에만 사용합니다.
 - 커밋 메시지의 민감한 부분(토큰·URL·이메일·티켓 번호·금지어)은
   이 컴퓨터에서 가립니다. 데이터는 git push 직후와 동기화할 때
   aline.team(Alineteam Inc., 미국)으로 전송되며
   가입일로부터 1년간 보관됩니다. 계속 이용 중이면 자동 연장됩니다.
 - aline.team 가입이 필요합니다.
 - 개인정보처리방침: https://aline.team/privacy

 계속하려면 Enter, 중단하려면 Ctrl+C
```

- 로고는 ASCII 문자만 사용 (Windows 콘솔·폰트 호환). 폭 80열 이내
- `init`과 `version`에서만 표시. 다른 명령과 훅에서는 표시하지 않음
- 정책 문구는 실제 동작과 반드시 일치시킨다. 기능이 바뀌면 모든 언어의 문구를 함께 수정

**1. aline.team 가입·로그인** (6.1, 필수): 이용약관·개인정보처리방침 고지(가입 시 동의로 간주) → 이메일 직접 입력 → 확인 코드 → 토큰 저장
- 동의하지 않거나 인증을 마치지 않으면 `init`을 종료. 이후 단계는 진행하지 않음

**2. 저장소 모음 경로 입력**

```
Enter the folder(s) where you keep your repositories (comma-separated).
Found: ~/Documents/Code, ~/IdeaProjects
[Enter = use found] >
```
- 후보: 홈 아래 흔한 경로 중 존재하는 것 (`Code`, `code`, `dev`, `src`, `projects`, `workspace`, `repos`, `IdeaProjects`, `Documents/Code`, `Documents/GitHub` 등)
- 후보가 없고 입력도 없으면 홈 디렉터리 전체 (깊이 5)
- `~` 확장, 존재하지 않는 경로는 다시 입력 요청
- 입력한 경로는 `config.json`에 저장해 다음 `init`에서 기본값으로 사용
- 인자로도 지정 가능: `gitfolio init ~/Code ~/work`

**3. 본인 커밋 식별 이메일 확인** (3.2)
```
Your commits will be identified by: dev@example.com (git config)
```
- 작업 이메일 추가 단계는 없음 (6.1.2 제외)

**4. 의존성 분석 기능 동의** (기본 N)

```
GitFolio can detect frameworks and libraries by reading package
manager files (package.json, go.mod, pom.xml, build.gradle, ...).
You will choose exactly which files may be read, per repository.
- Files are used only to detect dependencies.
- Only dependency names and versions are kept; file contents and
  paths are never stored or sent.
Enable dependency detection? [y/N]
```

**5. 저장소 탐색** (2의 경로 아래)
- 폴더 이름만 확인하며 파일 내용은 읽지 않음
- 건너뛰는 폴더: 숨김 폴더, `Library`, `node_modules`, `vendor`
- 저장소를 찾으면 그 안으로 더 들어가지 않음 (git 서브모듈·중첩 저장소는 별도 등록 대상 아님)
- worktree는 원본과 같은 저장소로 취급 (`git rev-parse --git-common-dir` 기준 중복 제거)
- macOS: `Documents`·`Desktop`·`Downloads` 아래 경로는 터미널 앱의 파일 접근 권한 팝업이 뜰 수 있음. 거부하면 해당 폴더는 건너뜀

**6. 후보 표시·선택**
- 본인 커밋이 1개 이상인 저장소만. 번호, 로컬 경로, 본인 커밋 수, 마지막 커밋 날짜
- 안내 문구: 경고 대신 사실과 해결 방법을 안내 — "선택한 저장소는 소스 코드 없이 커밋 정보만 전송하며, 커밋 메시지의 민감한 부분은 가려서 보냅니다. 커밋 메시지 속 고객사·사내 프로젝트명은 `gitfolio config mask add <단어>`로 가릴 수 있습니다."
- 번호 입력 (`1,3,5-7`, `all`, `none`) → 선택한 저장소마다 `add` 수행
- 탐색 결과는 저장하지 않음. 선택한 저장소만 등록

**7. 매니저 파일 승인**: 고른 저장소 전체를 3.5의 승인 화면 하나로

**8. 첫 동기화**: 등록한 저장소의 첫 동기화 실행. 수집의 기본은 커밋·push 훅이므로 예약 동기화는 `init`에서 묻지 않는다. 훅을 거치지 않는 작업 방식이 많은 사용자만 `gitfolio schedule HH:MM`으로 직접 켠다

### 7.3 `add`

1. 경로가 git 저장소인지 확인
2. 훅 디렉터리 확인 (`git rev-parse --git-path hooks`)
   - **husky 9** (`core.hooksPath = .husky/_`, husky가 만드는 커밋되지 않는 폴더, `h` 파일로 판별): `_/pre-push`·`_/post-commit` 스텁 맨 앞(shebang 다음)에 표시선으로 감싼 블록을 넣는다. husky 줄(`. h`)이 끝에서 `exit`하므로 앞이어야 하고, stdin은 건드리지 않는다(`</dev/null`). husky는 `npm install` 때 `_`를 다시 쓰므로 scan마다 블록을 다시 넣고, 그 사이 push는 다음 sync가 보낸다. 해제 시 블록만 지워 원래 스텁으로 되돌린다 (2026-10-01 사용자 결정)
   - 그 밖에 훅 디렉터리가 `.git` 밖이면(공유 `core.hooksPath`, 커밋되는 husky 5–8 `.husky` 등) 자동 설치하지 않고 안내. 그 저장소는 push 직후 전송 없이 `sync`·예약 동기화로 보낸다
3. 기존 `post-commit`·`pre-push` 훅이 있으면 `*.gitfolio-orig`로 이름을 바꾸고 체이닝
4. 의존성 기능이 켜져 있으면 매니저 파일 승인 (3.5)
5. 저장소 등록 → 최초 수집 (네트워크 없음)

### 7.4 훅 동작

| 훅 | 동작 | 제한 |
|---|---|---|
| `post-commit` | 환경변수로 에이전트 판별 → `커밋 해시, 에이전트` 한 줄 기록 | git log 조회·네트워크 없음, 즉시 종료 |
| `pre-push` | `git push` 프로세스 ID를 넘겨 백그라운드 프로세스를 띄우고 즉시 종료. 백그라운드가 push 종료를 기다린 뒤 수집 → 전송 (6.2) | 훅 자체는 네트워크 없음·즉시 종료, push 차단·지연 금지 |

훅 스크립트 규칙:
- 체이닝된 원래 훅을 **먼저** 실행하고, 원래 훅의 종료 코드는 그대로 전달 (원래 훅이 push를 막으면 막힘)
- gitfolio 자체 실패는 무시하고 항상 성공으로 종료
- `gitfolio` 실행 파일이 없으면 아무것도 하지 않음
- `pre-push`의 stdin은 원래 훅과 gitfolio 양쪽에 전달
- 비대화형이므로 사용자 확인이 필요한 일(새 매니저 파일)은 확인 대기로 미룸
- GUI git 클라이언트는 PATH가 짧을 수 있으므로 `gitfolio`를 PATH → `/opt/homebrew/bin` → `/usr/local/bin` → `~/.local/bin` → `~/go/bin` 순서로 찾음
- 사람이 직접 한 커밋·push, IDE·GUI 클라이언트, AI 에이전트 모두 같은 git 훅을 거치므로 도구와 무관하게 수집. `git push --no-verify`와 훅을 끄는 도구(Gemini CLI)는 훅을 건너뛰므로 예약·수동 동기화의 scan으로 보완
- 모든 데이터 쓰기는 잠금 파일(`lock`)을 잡고 수행. 10분 넘은 잠금은 비정상 종료로 보고 해제

### 7.5 `schedule`

| OS | 등록 방식 |
|---|---|
| macOS | `~/Library/LaunchAgents/team.aline.gitfolio.plist` (`StartCalendarInterval`, 잠자기 중 놓친 실행은 깨어날 때 실행) |
| Linux | `systemd --user` 타이머 (`Persistent=true`). systemd가 없으면 `crontab` 한 줄 |
| Windows | 작업 스케줄러: `schtasks /Create /XML`(UTF-16)로 `GitFolio\Sync` 작업 등록. 매일 지정 시각, 로그온 중일 때 사용자 권한으로 실행(암호 저장 없음), 놓친 시각은 다음 로그온 때 실행(`StartWhenAvailable`). `cmd /c`로 실행해 출력은 `schedule.log`에 남김 |

- `gitfolio schedule HH:MM`(현지 시각) 등록, `schedule off` 해제, 인자 없으면 예약 시각과 마지막 동기화 시각·결과(`sync.json`의 `lastSync`·`lastError`) 표시
- 실행 명령은 PATH의 `gitfolio`(Homebrew 링크라 업그레이드 후에도 유지), 없으면 현재 실행 파일. 등록 시점 셸의 `PATH`와 `GITFOLIO_LANG`을 넘겨 git 경로·화면 언어를 터미널과 같게 함
- 실행 기록: macOS·cron은 `gitfolio/schedule.log`, systemd는 `journalctl --user -u gitfolio-sync`
- Linux: systemd 등록에 실패하면(사용자 세션 버스 없음 등) cron으로 대체하고, crontab도 없으면 원인(systemd 오류)과 함께 "cron 설치 또는 직접 `sync`"를 안내. systemd로 등록되면 옛 cron 줄은 지움. crontab을 읽지 못하면(“no crontab” 외 오류) 덮어쓰지 않고 중단
- macOS: `~/Documents`·`~/Desktop` 등에 있는 저장소는 첫 예약 실행 때 macOS가 폴더 접근 허용을 묻는다 (실측: 응답까지 실행이 멈춤, 한 번 허용하면 이후 바로 실행). 등록 시 안내
- 예약 실행은 비대화형: 확인이 필요한 항목은 건너뜀 (지금은 sync에 확인 단계 없음). 로그인이 만료되면 게이트로 실패하고 다음 대화형 명령에서 재로그인 안내
- `remove`로 마지막 저장소를 해제하거나 `schedule off` 시 스케줄러 항목 삭제
- 예약 실행은 `gitfolio sync`이므로, 의존성 파일을 함께 읽을지는 `deps auto on|off`(scan·sync와 같은 옵션, 기본 off)를 따른다

### 7.6 `remove`

- 우리 훅 제거, `*.gitfolio-orig`가 있으면 원래 이름으로 복원
- `--purge`: 해당 저장소 로컬 데이터 삭제 + 다음 sync 때 서버 삭제 요청

### 7.7 `export`

- 로컬 데이터를 파일·화면으로 출력. JSON `{ "commits": [...], "dependencies": [...] }` (전송 내용과 동일한 형태)
- `dependencies`는 본인이 수정한 모듈의 의존성만, 저장소 `namespace`·`ecosystem`·`name`·`version`. 모듈 ID는 포함하지 않음
- 로컬 경로는 출력하지 않음. 저장소는 이름과 `namespace`(소유자/저장소)로 표시
- 이미 마스킹된 데이터만 출력

### 7.8 git 명령 기록 (`history`)

사용자가 "허용한 파일만 읽는다"를 gitfolio의 말이 아니라 git의 기록으로 확인할 수 있게 한다 (2026-10-01 사용자 요청).

- 기본 켜짐. gitfolio가 실행하는 모든 git 명령에 `GIT_TRACE=<데이터 폴더>/git-history.log`를 붙인다. 명령 한 줄은 git이 직접 쓴다
- git을 처음 실행하기 전에 실행마다 머리줄 하나(`# <날짜·시각> gitfolio <명령>`)를 gitfolio가 쓴다. git의 기록에는 날짜가 없어서다
- `gitfolio history`: 실행별로 날짜·gitfolio 명령, 그 아래 git 명령(시각, `built-in`·`exec` 줄). 기본 최근 20회, `--all` 전부. 원본 파일에는 git이 쓴 모든 줄이 남는다
- 크기 상한 기본 1MB(`config git-history-size`, 16KB–100MB). 넘으면 오래된 실행부터 지워 상한의 약 3/4로 줄인다(매 실행마다 다시 쓰지 않게). 자른 뒤에도 실행 머리줄부터 시작한다
- `config git-history off`: 기록 중단 + 파일 삭제. 사용자가 직접 `GIT_TRACE`를 설정하면 그쪽을 존중하고 기록에는 쓰지 않는다
- 사용자가 직접 확인하는 방법(README는 요약만): `gitfolio history --all | grep cat-file` → 승인한 매니저 파일만. 따로 기록하려면 `GIT_TRACE=$PWD/git-trace.txt gitfolio deps scan --all`(PowerShell: `$env:GIT_TRACE="$PWD\git-trace.txt"`). git을 거치지 않는 파일 열기까지 보려면 `strace -f -e trace=openat`(Linux), `sudo fs_usage -w -f filesys gitfolio`(macOS), Process Monitor(Windows). 보낼 내용은 `sync --dry-run`, 설치본 출처는 `gh attestation verify <파일> -R Alineteam-Inc/GitFolio`(v0.1.1부터)
- 한계: 동시에 도는 gitfolio 두 개(push 직후 전송과 수동 명령 등)의 줄은 섞일 수 있고, 자르는 순간 다른 실행이 쓴 한 줄이 빠질 수 있다. 파일은 데이터 폴더(본인만 접근) 안, 권한 0600

## 8. 로컬 저장 구조

위치: `os.UserConfigDir()/gitfolio/`
(macOS `~/Library/Application Support/gitfolio`, Linux `~/.config/gitfolio`, Windows `%AppData%\gitfolio`)

| 파일 | 내용 |
|---|---|
| `config.json` | 저장소 모음 경로, 금지어, 의존성 기능 동의, push 동기화 on/off(`autoSyncOff`), 서버 주소, 예약 시각 |
| `credentials.json` | aline.team CLI 토큰(`aln_cli_…`)·만료 시각·계정 이메일 (권한 0600) |
| `repos.json` | 등록 저장소: 로컬 경로, 이름, 마지막 수집 정보, 매니저 파일 승인·거절·대기 목록 (**로컬 전용, 전송 안 함**) |
| `commits.jsonl` | 커밋 1건당 1줄 |
| `agent-tags.jsonl` | `post-commit` 훅이 기록한 커밋 해시 → 에이전트 |
| `deps.json` | 저장소·모듈별 의존성 (의존성 기능 사용 시에만 생성) |
| `sync.json` | 전송한 계정, 레코드 ID(`provider/namespace/hash`)별 전송 지문(내용 해시), 대기 중인 저장소 삭제 요청, 마지막 동기화 시각·실패 사유 |
| `schedule.log` | 예약 동기화 실행 기록 (macOS·cron) |
| `git-history.log` | gitfolio가 실행한 git 명령 기록 (`GIT_TRACE`, 7.8). 기본 최대 1MB |

커밋 레코드 예:

```json
{"repo":"3f2a9c01b7de","hash":"119fcfa…","branch":"main","authorEmail":"me@example.com","date":"2026-09-27T10:00:00+09:00","message":"fix: [TICKET] 파서 수정","files":[{"name":"cmd/gitfolio/git.go","add":12,"del":3,"module":"m1"}],"creationType":"HUMAN_CO_AI","aiAgents":["claude-code"]}
```

(`repo`는 로컬 저장소 ID, `module`은 로컬 전용. 전송할 때는 둘 다 빠지고 저장소는 요청 최상위 `provider`·`namespace`로 간다)

## 9. 알려진 한계

- **Gemini CLI**: 자식 git 프로세스의 훅을 비활성화(`core.hooksPath=''`)하고 트레일러도 남기지 않아 AI 탐지 불가. 커밋 자체는 다음 scan에서 `human`으로 수집
- **트레일러**: 사용자가 끄거나 지우면 탐지 불가. Codex는 API 키 로그인 시 트레일러 없음
- **amend·rebase**: 해시가 바뀌면 `post-commit` 기록과 연결이 끊김
- **noreply 전용 커밋**: GitHub 웹 UI 커밋, 클라우드 에이전트가 noreply로 본인을 공동 작성자에 넣은 커밋은 수집되지 않음
- **마스킹**: 규칙 기반이라 완전하지 않음
- **의존성**: 지원 목록에 없는 매니저 파일, 동적으로 생성되는 의존성 선언(스크립트·플러그인)은 누락. 모듈 귀속은 추정치
- **공개 저장소**: 커밋 해시와 namespace를 보내므로, 공개 저장소는 해당 커밋과 코드를 누구나 찾아볼 수 있는 상태로 연결됨 (검증 목적상 의도된 동작)
- **예약 동기화**: 컴퓨터가 꺼져 있으면 실행되지 않음. 그래서 push 동기화가 기본이며, 예약은 보완용
- **URL로 push**: `git push https://…`처럼 원격 이름 없이 push하면 원격 추적 브랜치가 갱신되지 않아 push 성공을 확인할 수 없음 → 해당 커밋은 수집되지 않음
- **push 직후 기기 종료**: 백그라운드 전송 전에 꺼지면 다음 push·동기화 때 전송

## 10. 아이디어 (미정)

- **스택 추정**: 파일명·확장자로 언어 비율, `Dockerfile` 등 파일명으로 도구 추정
- **의존성 → 프레임워크 매핑**: `spring-boot-starter-*` → Spring Boot, `react` → React 등 대표 매핑 표
- **의존성 이력**: 과거 시점의 매니저 파일을 읽어 기간별 스택 변화 추적 (파일 읽기 승인 범위 확장 필요)
- **`stats` 명령**: 언어 비율, 월별 활동, AI 활용 비율을 터미널에 요약
- **`post-rewrite` 훅**: amend·rebase 시 에이전트 기록을 새 해시로 이전
- **OS 키체인**: 토큰을 macOS Keychain·Linux Secret Service·Windows Credential Manager에 저장
- **배포 확장**: Windows(scoop/winget), homebrew-core, macOS 공증

## 11. 미결 사항

| # | 항목 | 상태 | 필요한 것 | 막히는 단계 |
|---|---|---|---|---|
| 1 | aline.team 데이터 API | ✅ 합의 v3, 서버 1차 구현 중 | 1차 커밋 전송(`branch` 필수)·저장소 삭제, 2차 의존성, 작업 이메일 인증 제외. 브랜치 기록 방식 확인 중 | ROADMAP 7 |
| — | 국외 이전 고지 문구 | ✅ 완료 | 법무 검토 조항은 https://aline.team/privacy 에 반영됨. CLI 동의 화면은 이 방침과 일치시킴 | |
| — | 저장소 | ✅ 확정 | CLI는 namespace(`소유자/저장소`)를 보내고 aline.team 서버가 git 서비스 API로 조회 (3.1) | |
| 3 | 데스크톱 앱 범위·일정 | 미정 | 앱이 CLI를 호출하는 방식(6.4)으로 갈지 | 이후 |
| 4 | 일본어 문구 | 미정 | 원어민 검수 | ROADMAP 9 |
| — | 본인 커밋 식별 이메일 | ✅ 확정 | 저장소별 `git config user.email`. 작업 이메일 인증은 제외 (3.2, 6.1.2) | |
| — | 가입 전 동작 | ✅ 확정 | 가입·로그인 전에는 수집·동작 없음 (6.1) | |
| — | 보관 기간 | ✅ 확정 | 가입일로부터 1년, 이용 중이면 자동 연장 (6.3) | |
| — | 인증 방식 | ✅ 확정 (서버 결정) | 이메일 코드 로그인 + 불투명 토큰 `aln_cli_…`(활동 시 연장, 일정 기간 비활동 시 폐기). 기기 키 방식은 채택하지 않음 (6.1) | |
| — | 계정 생성 | ✅ 확정 | 가입 필수, 직접 입력·확인한 이메일만. `git config user.email` 사용 안 함 | |
| — | 개인정보처리방침 URL | ✅ 확정 | https://aline.team/privacy | |
| — | 보관 지역 | ✅ 확정 | GCP, 미국 (리전은 표기하지 않음, 2026-10-01) | |
| — | 수동 동기화 수단 | ✅ 확정 | CLI `gitfolio sync`, 데스크톱 앱 버튼 | |
