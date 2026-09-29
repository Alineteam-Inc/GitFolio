package main

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// detectLang picks the screen language (DESIGN 7.1): GITFOLIO_LANG if set, else the device's language
// (on macOS the first one GitFolio has in System Settings › Language & Region, because terminals often
// set LANG to en_US whatever the device language is), else LC_ALL, LC_MESSAGES or LANG, else English.
// ponytail: Windows needs an OS API call (GetUserDefaultUILanguage) once Windows is supported.
func detectLang(getenv func(string) string) string {
	if v := strings.TrimSpace(getenv("GITFOLIO_LANG")); v != "" {
		return cmp.Or(langOf(v), "en")
	}
	for _, v := range osLanguages() {
		if l := langOf(v); l != "" {
			return l
		}
	}
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return cmp.Or(langOf(v), "en") // the first variable that is set wins, as in POSIX
		}
	}
	return "en"
}

// langOf maps a locale or language tag ("ko_KR.UTF-8", "ja-JP", "en-GB") to ko, ja or en; "" for others.
func langOf(v string) string {
	v = strings.ToLower(v)
	for _, l := range []string{"ko", "ja", "en"} {
		if strings.HasPrefix(v, l) {
			return l
		}
	}
	return ""
}

// osLanguages returns the device's preferred languages, most preferred first, read once per run.
// On Linux LANG and LC_* already are the device setting.
var osLanguages = sync.OnceValue(func() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	out, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output()
	if err != nil {
		return nil
	}
	return parseAppleLanguages(string(out))
})

// parseAppleLanguages reads the output of `defaults read -g AppleLanguages`: ( "ko-KR", "en-US" ), one per line.
func parseAppleLanguages(out string) []string {
	var langs []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.Trim(strings.TrimSpace(l), `"(),`); l != "" {
			langs = append(langs, l)
		}
	}
	return langs
}

// statusPrefix marks GitFolio's own result and error lines so they stand out among shell and git output.
// Prompts and notices carry only the margin (a prefix would blur where to type).
const statusPrefix = "GitFolio-cli >>> "

// margin keeps GitFolio's interactive text off the terminal's left edge.
const margin = "  "

// indent puts the margin in front of every non-empty line of s.
func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = margin + l
		}
	}
	return strings.Join(lines, "\n")
}

// say prints a translated result line with the GitFolio prefix.
func say(lang, key string, args ...any) { show(os.Stdout, fmt.Sprintf(tr(lang, key), args...)) }

// warn is say for problems, on stderr.
func warn(lang, key string, args ...any) { show(os.Stderr, fmt.Sprintf(tr(lang, key), args...)) }

// show prints text with the GitFolio prefix on its first line and the other lines aligned under it.
func show(w io.Writer, text string) {
	under := strings.Repeat(" ", len(statusPrefix))
	for i, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if i == 0 {
			fmt.Fprintln(w, margin+statusPrefix+line)
		} else {
			fmt.Fprintln(w, margin+under+line)
		}
	}
}

// failure returns a translated error; main prints it with the GitFolio prefix. Errors that come from
// git itself or only show up in hooks stay as they are.
func failure(key string, args ...any) error {
	return errors.New(strings.TrimSpace(fmt.Sprintf(tr(detectLang(os.Getenv), key), args...)))
}

// section prints the separator that opens an interactive command.
func section(lang, titleKey string) {
	fmt.Printf("\n%s===== GitFolio · %s =====\n", margin, strings.TrimSpace(tr(lang, titleKey)))
}

// tr returns the message for lang, falling back to English.
func tr(lang, key string) string {
	if s, ok := messages[key][lang]; ok {
		return s
	}
	return messages[key]["en"]
}

// messages holds the text people read. Policy and consent texts must mean the same in every language
// and match what the program really does; change all languages together. Japanese needs a native review.
var messages = map[string]map[string]string{
	"policy": {
		"en": ` [Data Policy]
 - Source code is never collected.
 - Collected: commit hashes, author emails, commit messages
   (masked), branch names, timestamps, file names, lines
   added/deleted, AI usage, and repository namespaces (owner/repo).
 - Files are read only with your approval, and only package
   manager files, only to detect dependencies.
 - Sensitive parts of commit messages (tokens, URLs, emails,
   ticket numbers, blocked words) are masked on this computer.
   Data is sent to aline.team (Alineteam Inc., United States)
   after each git push and when you sync. Kept for 1 year from
   sign-up, renewed automatically while you keep using GitFolio.
 - An aline.team account is required.
 - Privacy policy: https://aline.team/privacy
`,
		"ko": ` [데이터 정책]
 - 소스 코드는 수집하지 않습니다.
 - 수집 항목: 커밋 해시, 작성자 이메일, 커밋 메시지(마스킹), 브랜치 이름,
   시점, 파일명, 추가·삭제 줄 수, AI 사용 여부, 저장소 namespace(소유자/저장소)
 - 파일 읽기는 사용자가 승인한 패키지 매니저 파일에 한하며,
   의존성 파악에만 사용합니다.
 - 커밋 메시지의 민감한 부분(토큰·URL·이메일·티켓 번호·금지어)은
   이 컴퓨터에서 가립니다. 데이터는 git push 직후와 동기화할 때
   aline.team(Alineteam Inc., 미국)으로 전송되며
   가입일로부터 1년간 보관됩니다. 계속 이용 중이면 자동 연장됩니다.
 - aline.team 가입이 필요합니다.
 - 개인정보처리방침: https://aline.team/privacy
`,
		"ja": ` [データポリシー]
 - ソースコードは収集しません。
 - 収集項目: コミットハッシュ、作成者のメールアドレス、
   コミットメッセージ(マスキング済み)、ブランチ名、日時、ファイル名、
   追加・削除行数、AI の利用有無、リポジトリ(オーナー/リポジトリ)
 - ファイルの読み取りは、承認されたパッケージマネージャーの
   ファイルに限り、依存関係の把握にのみ使用します。
 - コミットメッセージの機密部分(トークン・URL・メールアドレス・
   チケット番号・ブロックワード)はこのコンピューターでマスキングします。
   データは git push の直後と同期時に aline.team(Alineteam Inc.、米国)へ
   送信され、登録日から1年間保管されます。ご利用中は自動的に延長されます。
 - aline.team のアカウントが必要です。
 - プライバシーポリシー: https://aline.team/privacy
`,
	},
	"pressEnter": {
		"en": " Press Enter to continue, or Ctrl+C to quit. ",
		"ko": " 계속하려면 Enter, 중단하려면 Ctrl+C ",
		"ja": " 続行するには Enter、中止するには Ctrl+C ",
	},
	"rootsAsk": {
		"en": "\nWhich folders hold your repositories? (comma-separated)\n",
		"ko": "\n저장소를 모아 둔 폴더를 입력하세요. (여러 개는 쉼표로 구분)\n",
		"ja": "\nリポジトリを置いているフォルダーを入力してください。(複数はカンマ区切り)\n",
	},
	"rootsFound": {
		"en": "Found: %s\n",
		"ko": "찾은 폴더: %s\n",
		"ja": "見つかったフォルダー: %s\n",
	},
	"rootsPrompt": {
		"en": "[Enter = use these, or your home folder if none] > ",
		"ko": "[Enter = 위 폴더 사용, 없으면 홈 폴더 전체] > ",
		"ja": "[Enter = 上記を使用、なければホームフォルダー全体] > ",
	},
	"rootMissing": {
		"en": "Folder not found: %s\n",
		"ko": "폴더를 찾을 수 없습니다: %s\n",
		"ja": "フォルダーが見つかりません: %s\n",
	},
	"identityEmail": {
		"en": "\nYour commits are identified by: %s (git config)\n",
		"ko": "\n본인 커밋은 이 이메일로 식별합니다: %s (git config)\n",
		"ja": "\nあなたのコミットはこのメールアドレスで識別します: %s (git config)\n",
	},
	"identityMissing": {
		"en": "\ngit user.email is not set, so your commits cannot be identified.\nSet it with: git config --global user.email you@example.com\n",
		"ko": "\ngit user.email이 설정되어 있지 않아 본인 커밋을 식별할 수 없습니다.\n설정 방법: git config --global user.email you@example.com\n",
		"ja": "\ngit の user.email が設定されていないため、あなたのコミットを識別できません。\n設定方法: git config --global user.email you@example.com\n",
	},
	"depsNotice": {
		"en": "Dependency detection reads the package manager files you select (package.json, go.mod,\npom.xml, build.gradle, ...) in each repository, only to detect dependencies.\nOnly dependency names and versions are kept; file contents and paths are never stored or sent.\n",
		"ko": "의존성 분석은 저장소마다 사용자가 고른 패키지 매니저 파일(package.json, go.mod,\npom.xml, build.gradle 등)만, 의존성 파악 용도로만 읽습니다.\n의존성 이름과 버전만 남기며, 파일 원문과 경로는 저장하지도 전송하지도 않습니다.\n",
		"ja": "依存関係の分析では、リポジトリごとにあなたが選んだパッケージマネージャーのファイル\n(package.json、go.mod、pom.xml、build.gradle など)のみを、依存関係の把握のためだけに読み取ります。\n依存関係の名前とバージョンのみを保持し、ファイルの内容とパスは保存も送信もしません。\n",
	},
	"depsAsk": {
		"en": "Turn on dependency detection? You will choose the files per repository.",
		"ko": "의존성 분석을 켤까요? 읽을 파일은 저장소마다 직접 고릅니다.",
		"ja": "依存関係の分析を有効にしますか? 読み取るファイルはリポジトリごとに選びます。",
	},
	"noneSelected": {
		"en": "No repositories were chosen. Run `gitfolio init` again any time to choose.\n",
		"ko": "선택한 저장소가 없습니다. 언제든 `gitfolio init`을 다시 실행해 고를 수 있습니다.\n",
		"ja": "リポジトリは選択されませんでした。いつでも `gitfolio init` を再実行して選べます。\n",
	},
	"searching": {
		"en": "Looking for git repositories (folder names only; macOS may ask for folder access)...\n",
		"ko": "git 저장소를 찾는 중입니다. (폴더 이름만 확인하며, macOS가 폴더 접근 권한을 물을 수 있습니다)\n",
		"ja": "git リポジトリを検索しています。(フォルダー名のみ確認します。macOS がフォルダーへのアクセス許可を求める場合があります)\n",
	},
	"noCandidates": {
		"en": "No new repositories with your commits were found.\n",
		"ko": "본인 커밋이 있는 새 저장소를 찾지 못했습니다.\n",
		"ja": "あなたのコミットがある新しいリポジトリは見つかりませんでした。\n",
	},
	"companyNotice": {
		"en": "Repositories with your commits are listed above (commits, latest date).\nFrom the chosen ones, commit information is sent, never source code; sensitive parts of commit messages are masked.\nHide customer or internal project names in commit messages with `gitfolio config mask add <word>`.\n",
		"ko": "위는 본인 커밋이 있는 저장소입니다. (커밋 수, 최근 커밋 날짜)\n선택한 저장소는 소스 코드 없이 커밋 정보만 전송하며, 커밋 메시지의 민감한 부분은 가려서 보냅니다.\n커밋 메시지 속 고객사·사내 프로젝트명은 `gitfolio config mask add <단어>`로 가릴 수 있습니다.\n",
		"ja": "上記はあなたのコミットがあるリポジトリです。(コミット数、最新コミット日)\n選択したリポジトリからは、ソースコードではなくコミット情報のみを送信し、コミットメッセージの機密部分はマスキングします。\nコミットメッセージ内の顧客名や社内プロジェクト名は `gitfolio config mask add <単語>` で隠せます。\n",
	},
	"selectRepos": {
		"en": "Which repositories should GitFolio collect? [all / 1,3,5-7 / Enter = none] > ",
		"ko": "어느 저장소를 수집할까요? [all / 1,3,5-7 / Enter = 선택 안 함] > ",
		"ja": "どのリポジトリを収集しますか? [all / 1,3,5-7 / Enter = 選択しない] > ",
	},
	"chooseLater": {
		"en": "Run `gitfolio init` in a terminal to choose, or `gitfolio add <path>` for one repository.\n",
		"ko": "터미널에서 `gitfolio init`을 실행해 고르거나, `gitfolio add <경로>`로 하나씩 등록하세요.\n",
		"ja": "ターミナルで `gitfolio init` を実行して選ぶか、`gitfolio add <パス>` で1件ずつ登録してください。\n",
	},
	"initDone": {
		"en": "Setup is done.\nRepositories: %d\nSend right after git push: %s\nDependency detection: %s\nDaily sync: %s\nChange these any time by running `gitfolio init` again, or with `gitfolio config`, `deps` and `schedule`.\n",
		"ko": "설정을 마쳤습니다.\n등록 저장소: %d개\npush 직후 자동 전송: %s\n의존성 분석: %s\n예약 동기화: %s\n언제든 `gitfolio init`을 다시 실행하거나 `gitfolio config`·`deps`·`schedule`로 바꿀 수 있습니다.\n",
		"ja": "設定が完了しました。\n登録リポジトリ: %d 件\ngit push 直後の自動送信: %s\n依存関係の分析: %s\n予約同期: %s\nいつでも `gitfolio init` を再実行するか、`gitfolio config`・`deps`・`schedule` で変更できます。\n",
	},
	"signupNotice": {
		"en": "There is no aline.team account for %s, so a new one will be created.\nSigning up means you agree to\n  Terms of Service: https://aline.team/terms\n  Privacy Policy:   https://aline.team/privacy\n",
		"ko": "%s(으)로 된 aline.team 계정이 없어 새 계정을 만듭니다.\n가입하면 아래 내용에 동의한 것으로 간주합니다.\n  이용약관: https://aline.team/terms\n  개인정보처리방침: https://aline.team/privacy\n",
		"ja": "%s の aline.team アカウントがないため、新しいアカウントを作成します。\n登録すると、以下に同意したものとみなされます。\n  利用規約: https://aline.team/terms\n  プライバシーポリシー: https://aline.team/privacy\n",
	},
	"signupAsk": {
		"en": "Create the account? (check the email for typos) [Y/n] > ",
		"ko": "계정을 만들까요? (이메일에 오타가 없는지 확인하세요) [Y/n] > ",
		"ja": "アカウントを作成しますか?(メールアドレスに誤りがないかご確認ください) [Y/n] > ",
	},
	"notifyAsk": {
		"en": "Receive aline.team service notifications by email? [y/N] > ",
		"ko": "aline.team 서비스 알림을 이메일로 받을까요? [y/N] > ",
		"ja": "aline.team のサービス通知をメールで受け取りますか? [y/N] > ",
	},
	"signupCancelled": {
		"en": "No account was created. Run `gitfolio login` again with the right email.\n",
		"ko": "계정을 만들지 않았습니다. 올바른 이메일로 `gitfolio login`을 다시 실행하세요.\n",
		"ja": "アカウントは作成していません。正しいメールアドレスで `gitfolio login` を再実行してください。\n",
	},
	"emailAsk": {
		"en": "Your aline.team email > ",
		"ko": "aline.team 이메일 > ",
		"ja": "aline.team のメールアドレス > ",
	},
	"emailInvalid": {
		"en": "Please enter a valid email address.\n",
		"ko": "올바른 이메일 주소를 입력하세요.\n",
		"ja": "正しいメールアドレスを入力してください。\n",
	},
	"codeAsk": {
		"en": "Enter the code sent to %s > ",
		"ko": "%s(으)로 보낸 확인 코드를 입력하세요 > ",
		"ja": "%s に送信した確認コードを入力してください > ",
	},
	"codeFormat": {
		"en": "The code is %d digits. Please type it again.\n",
		"ko": "확인 코드는 숫자 %d자리입니다. 다시 입력하세요.\n",
		"ja": "確認コードは %d 桁の数字です。もう一度入力してください。\n",
	},
	"codeWrong": {
		"en": "The code is not correct. Please try again.\n",
		"ko": "확인 코드가 맞지 않습니다. 다시 입력하세요.\n",
		"ja": "確認コードが正しくありません。もう一度入力してください。\n",
	},
	"signedUp": {
		"en": "Welcome! Your aline.team account %s was created and this device is signed in.\n",
		"ko": "aline.team 계정 %s 가입을 마쳤고, 이 기기에서 로그인했습니다.\n",
		"ja": "aline.team アカウント %s の登録が完了し、このデバイスでログインしました。\n",
	},
	// aline.team mails a one-time link (24 hours) after a CLI sign-up, since the account has no web password.
	"passwordMail": {
		"en": "To log in on the web (aline.team), set a password with the link we emailed you.\nThe link works once within 24 hours; after that, use \"Forgot password\" on the website.\n",
		"ko": "웹(aline.team) 로그인용 비밀번호 설정 메일을 보냈습니다.\n메일의 링크는 24시간 동안 한 번 쓸 수 있고, 만료되면 웹의 \"비밀번호 찾기\"로 설정하세요.\n",
		"ja": "Web(aline.team)ログイン用のパスワード設定メールを送信しました。\nリンクは24時間以内に1回のみ使えます。期限切れの場合は、Web の「パスワードをお忘れですか」から設定してください。\n",
	},
	"loggedIn": {
		"en": "Logged in as %s.\n",
		"ko": "%s(으)로 로그인했습니다.\n",
		"ja": "%s でログインしました。\n",
	},
	"alreadyLoggedIn": {
		"en": "Already logged in as %s. Run `gitfolio logout` first to switch accounts.\n",
		"ko": "이미 %s(으)로 로그인되어 있습니다. 계정을 바꾸려면 먼저 `gitfolio logout`을 실행하세요.\n",
		"ja": "すでに %s でログインしています。アカウントを切り替えるには先に `gitfolio logout` を実行してください。\n",
	},
	"loginNeedsTerminal": {
		"en": "Logging in needs a terminal: run `gitfolio login` there.\n",
		"ko": "로그인은 터미널에서 해야 합니다. 터미널에서 `gitfolio login`을 실행하세요.\n",
		"ja": "ログインにはターミナルが必要です。ターミナルで `gitfolio login` を実行してください。\n",
	},
	"loginTitle": {
		"en": "Log in",
		"ko": "로그인",
		"ja": "ログイン",
	},
	"notLoggedIn": {
		"en": "Not logged in. Run `gitfolio login`.\n",
		"ko": "로그인되어 있지 않습니다. `gitfolio login`을 실행하세요.\n",
		"ja": "ログインしていません。`gitfolio login` を実行してください。\n",
	},
	"whoami": {
		"en": "Logged in as %s\nVerified emails: %s\nServer: %s\n",
		"ko": "로그인 계정: %s\n인증된 이메일: %s\n서버: %s\n",
		"ja": "ログイン中のアカウント: %s\n認証済みメールアドレス: %s\nサーバー: %s\n",
	},
	"loggedOut": {
		"en": "Logged out. This device's token was revoked and removed.\n",
		"ko": "로그아웃했습니다. 이 기기의 토큰을 폐기하고 삭제했습니다.\n",
		"ja": "ログアウトしました。このデバイスのトークンを失効させ、削除しました。\n",
	},
	"loginFirst": {
		"en": "Log in to aline.team first: gitfolio login\n",
		"ko": "먼저 aline.team에 로그인하세요: gitfolio login\n",
		"ja": "先に aline.team にログインしてください: gitfolio login\n",
	},
	"relogin": {
		"en": "(run `gitfolio login`)",
		"ko": "(`gitfolio login`으로 다시 로그인하세요)",
		"ja": "(`gitfolio login` で再ログインしてください)",
	},
	"apiURLInvalid": {
		"en": "API URL %q is not a valid URL.\n",
		"ko": "올바른 URL이 아닙니다: %q\n",
		"ja": "API URL %q は正しい URL ではありません。\n",
	},
	"apiURLHTTPS": {
		"en": "API URL %q must use https (plain http is allowed only to this computer).\n",
		"ko": "API 주소는 https만 쓸 수 있습니다 (http는 이 컴퓨터 주소만 허용): %q\n",
		"ja": "API URL %q は https である必要があります(http はこのコンピューターのアドレスのみ可)。\n",
	},
	"noGit": {
		"en": "git was not found. Install git first.\n",
		"ko": "git을 찾을 수 없습니다. 먼저 git을 설치하세요.\n",
		"ja": "git が見つかりません。先に git をインストールしてください。\n",
	},
	"notGitRepo": {
		"en": "%s is not a git repository.\n",
		"ko": "git 저장소가 아닙니다: %s\n",
		"ja": "%s は git リポジトリではありません。\n",
	},
	"noUserEmail": {
		"en": "git user.email is not set, so your commits cannot be identified.\nSet it with: git config --global user.email you@example.com\n",
		"ko": "git user.email이 설정되어 있지 않아 본인 커밋을 식별할 수 없습니다.\n설정 방법: git config --global user.email you@example.com\n",
		"ja": "git の user.email が設定されていないため、あなたのコミットを識別できません。\n設定方法: git config --global user.email you@example.com\n",
	},
	"alreadyRegistered": {
		"en": "%s is already registered.\n",
		"ko": "이미 등록된 저장소입니다: %s\n",
		"ja": "%s はすでに登録されています。\n",
	},
	"notRegistered": {
		"en": "%s is not registered.\n",
		"ko": "등록되지 않은 저장소입니다: %s\n",
		"ja": "%s は登録されていません。\n",
	},
	"notRegisteredAdd": {
		"en": "%s is not registered. Register it with `gitfolio add`.\n",
		"ko": "등록되지 않은 저장소입니다: %s\n`gitfolio add`로 등록하세요.\n",
		"ja": "%s は登録されていません。`gitfolio add` で登録してください。\n",
	},
	"scanFailed": {
		"en": "Some repositories could not be scanned (see above).\n",
		"ko": "일부 저장소를 수집하지 못했습니다. (위 내용 참고)\n",
		"ja": "一部のリポジトリを収集できませんでした。(上記を参照)\n",
	},
	"depsIsOff": {
		"en": "Dependency detection is off. Turn it on with `gitfolio deps on`.\n",
		"ko": "의존성 분석이 꺼져 있습니다. `gitfolio deps on`으로 켜세요.\n",
		"ja": "依存関係の分析はオフです。`gitfolio deps on` で有効にしてください。\n",
	},
	"unknownCommand": {
		"en": "Unknown command %q. See `gitfolio help`.\n",
		"ko": "알 수 없는 명령입니다: %q\n`gitfolio help`에서 명령 목록을 확인하세요.\n",
		"ja": "不明なコマンド %q です。`gitfolio help` をご確認ください。\n",
	},
	"usage": {
		"en": "Usage: %s\n",
		"ko": "사용법: %s\n",
		"ja": "使い方: %s\n",
	},
	"busy": {
		"en": "Another gitfolio run is in progress; try again shortly.\n",
		"ko": "다른 gitfolio 작업이 진행 중입니다. 잠시 후 다시 시도하세요.\n",
		"ja": "別の gitfolio の処理が実行中です。しばらくしてから再試行してください。\n",
	},
	"hooksElsewhere": {
		"en": "Hooks live outside .git (%s, e.g. core.hooksPath or husky), so they were not changed.\nAdd these lines yourself:\n  pre-push:    gitfolio hook pre-push \"$PPID\"\n  post-commit: gitfolio hook post-commit\n",
		"ko": "훅 폴더가 .git 밖에 있어(%s, 예: core.hooksPath·husky) 훅을 바꾸지 않았습니다.\n아래 줄을 직접 추가하세요:\n  pre-push:    gitfolio hook pre-push \"$PPID\"\n  post-commit: gitfolio hook post-commit\n",
		"ja": "フックが .git の外にあるため(%s、例: core.hooksPath・husky)、変更していません。\n次の行をご自身で追加してください:\n  pre-push:    gitfolio hook pre-push \"$PPID\"\n  post-commit: gitfolio hook post-commit\n",
	},
	"hooksBothExist": {
		"en": "Both %[1]s and %[1]s.gitfolio-orig exist; merge them by hand.\n",
		"ko": "기존 훅과 백업이 모두 있어 직접 하나로 합쳐야 합니다: %[1]s, %[1]s.gitfolio-orig\n",
		"ja": "%[1]s と %[1]s.gitfolio-orig の両方があります。手動で1つにまとめてください。\n",
	},
	"logoutServerFailed": {
		"en": "Could not log out on the server (%v); the token on this device is removed anyway.\n",
		"ko": "서버 로그아웃에 실패했지만(%v) 이 기기의 토큰은 삭제합니다.\n",
		"ja": "サーバーでのログアウトに失敗しましたが(%v)、このデバイスのトークンは削除します。\n",
	},
	"failed": {
		"en": "Error: %v\n",
		"ko": "오류: %v\n",
		"ja": "エラー: %v\n",
	},
	"repoFailed": {
		"en": "%s: %v\n",
		"ko": "%s: %v\n",
		"ja": "%s: %v\n",
	},
	"registered": {
		"en": "Registered %s (%d commit(s)).\n",
		"ko": "%s 저장소를 등록했습니다. (커밋 %d개)\n",
		"ja": "%s を登録しました。(コミット %d 件)\n",
	},
	"hooksNotInstalled": {
		"en": "Git hooks were not installed: %v\nCommits are still collected by `gitfolio scan`.\n",
		"ko": "git hook을 설치하지 못했습니다: %v\n`gitfolio scan`으로는 계속 수집할 수 있습니다.\n",
		"ja": "git フックをインストールできませんでした: %v\n`gitfolio scan` では引き続き収集できます。\n",
	},
	"hooksNotRestored": {
		"en": "Git hooks were not restored: %v\n",
		"ko": "git hook을 원래대로 되돌리지 못했습니다: %v\n",
		"ja": "git フックを元に戻せませんでした: %v\n",
	},
	"removed": {
		"en": "Removed %s.\n",
		"ko": "%s 저장소 등록을 해제했습니다.\n",
		"ja": "%s の登録を解除しました。\n",
	},
	"scanned": {
		"en": "%s: %d new commit(s)\n",
		"ko": "%s: 새 커밋 %d개\n",
		"ja": "%s: 新しいコミット %d 件\n",
	},
	"depsPending": {
		"en": "Package manager files waiting for your review: %d (gitfolio deps review %s)\n",
		"ko": "패키지 매니저 파일 %d개가 검토를 기다립니다: gitfolio deps review %s\n",
		"ja": "パッケージマネージャーのファイル %d 件が確認待ちです: gitfolio deps review %s\n",
	},
	"apiURL": {
		"en": "aline.team API: %s\n",
		"ko": "aline.team API: %s\n",
		"ja": "aline.team API: %s\n",
	},
	"maskRemoved": {
		"en": "Removed. Already masked data stays masked; run `gitfolio scan --all --rebuild` to collect it again.\n",
		"ko": "삭제했습니다. 이미 마스킹된 데이터는 그대로이며, 다시 수집하려면 `gitfolio scan --all --rebuild`를 실행하세요.\n",
		"ja": "削除しました。すでにマスキングされたデータはそのままです。再収集するには `gitfolio scan --all --rebuild` を実行してください。\n",
	},
	"maskApplied": {
		"en": "Applied to %d stored commit(s).\n",
		"ko": "저장된 커밋 %d개에 적용했습니다.\n",
		"ja": "保存済みのコミット %d 件に適用しました。\n",
	},
	"tooManyManifests": {
		"en": "More than %d package manager files; the rest are ignored.\n",
		"ko": "패키지 매니저 파일이 %d개를 넘어 나머지는 무시합니다.\n",
		"ja": "パッケージマネージャーのファイルが %d 件を超えたため、残りは無視します。\n",
	},
	"noManifests": {
		"en": "%s: no package manager files found.\n",
		"ko": "%s: 패키지 매니저 파일이 없습니다.\n",
		"ja": "%s: パッケージマネージャーのファイルはありません。\n",
	},
	"manifestsTitle": {
		"en": "Package manager files in %s. Selected files are read only to detect dependencies:\n",
		"ko": "%s의 패키지 매니저 파일입니다. 고른 파일은 의존성 파악에만 읽습니다.\n",
		"ja": "%s のパッケージマネージャーのファイルです。選んだファイルは依存関係の把握にのみ読み取ります。\n",
	},
	// The three states have the same width within each language, so the file list stays aligned.
	"manifestNew": {
		"en": "new",
		"ko": "신규",
		"ja": "新規",
	},
	"manifestAllowed": {
		"en": "allowed",
		"ko": "허용",
		"ja": "許可",
	},
	"manifestDeclined": {
		"en": "declined",
		"ko": "거부",
		"ja": "拒否",
	},
	"manifestsAsk": {
		"en": "Allow reading which files? [all / none / 1,3,5-7 / Enter = keep as is] > ",
		"ko": "읽어도 되는 파일을 고르세요. [all / none / 1,3,5-7 / Enter = 그대로 두기] > ",
		"ja": "読み取りを許可するファイルを選んでください。[all / none / 1,3,5-7 / Enter = 変更しない] > ",
	},
	"badSelection": {
		"en": "Use all, none, or numbers between 1 and %d like 1,3,5-7.\n",
		"ko": "all, none 또는 1,3,5-7처럼 1~%d 사이의 번호로 입력하세요.\n",
		"ja": "all、none、または 1,3,5-7 のように 1〜%d の番号で入力してください。\n",
	},
	"depsStatus": {
		"en": "Dependency detection: %s\n",
		"ko": "의존성 분석: %s\n",
		"ja": "依存関係の分析: %s\n",
	},
	"on": {
		"en": "on",
		"ko": "켜짐",
		"ja": "オン",
	},
	"off": {
		"en": "off",
		"ko": "꺼짐",
		"ja": "オフ",
	},
	"depsRepo": {
		"en": "%s: %d allowed, %d waiting for review\n",
		"ko": "%s: 허용 %d개, 검토 대기 %d개\n",
		"ja": "%s: 許可 %d 件、確認待ち %d 件\n",
	},
	"depsReviewLater": {
		"en": "Run `gitfolio deps review` in each repository to choose files.\n",
		"ko": "저장소마다 `gitfolio deps review`를 실행해 읽을 파일을 고르세요.\n",
		"ja": "リポジトリごとに `gitfolio deps review` を実行して、読み取るファイルを選んでください。\n",
	},
	"depsOff": {
		"en": "Dependency detection is off; collected dependencies were deleted.\n",
		"ko": "의존성 분석을 껐고, 수집한 의존성은 삭제했습니다.\n",
		"ja": "依存関係の分析をオフにし、収集した依存関係を削除しました。\n",
	},
	"synced": {
		"en": "Synced with aline.team: %d commit(s) sent, %d repository deletion(s).\nDashboards and profiles show them after aline.team's next analysis run.\n",
		"ko": "aline.team에 동기화했습니다: 커밋 %d개 전송, 저장소 삭제 %d건\n대시보드·프로필에는 aline.team의 다음 분석 때 반영됩니다.\n",
		"ja": "aline.team と同期しました: コミット %d 件を送信、リポジトリ削除 %d 件\nダッシュボード・プロフィールには aline.team の次回の分析時に反映されます。\n",
	},
	"syncLater": {
		"en": "Could not send to aline.team: %v\nNothing is lost: what was not sent goes with the next push or sync.\n",
		"ko": "aline.team에 보내지 못했습니다: %v\n보내지 못한 데이터는 다음 push나 sync 때 다시 보냅니다.\n",
		"ja": "aline.team に送信できませんでした: %v\n送信できなかったデータは、次の push または sync の際に再送します。\n",
	},
	"upToDate": {
		"en": "aline.team is already up to date.\n",
		"ko": "aline.team과 이미 동기화되어 있습니다.\n",
		"ja": "aline.team とはすでに同期済みです。\n",
	},
	"noRemote": {
		"en": "%d commit(s) are not sent because their repository has no usable remote on a git service.\n",
		"ko": "쓸 수 있는 git 서비스 원격 저장소가 없는 저장소의 커밋 %d개는 보내지 않습니다.\n",
		"ja": "利用できる git サービスのリモートがないリポジトリのコミット %d 件は送信しません。\n",
	},
	"commitRejected": {
		"en": "aline.team did not accept commit %[2]s of %[1]s; it is skipped until it changes: %[3]v\n",
		"ko": "aline.team이 %[1]s의 커밋 %[2]s를 받지 않아 건너뜁니다. 내용이 바뀌면 다시 보냅니다: %[3]v\n",
		"ja": "aline.team が %[1]s のコミット %[2]s を受け付けなかったため、スキップします。内容が変われば再送します: %[3]v\n",
	},
	"autosync": {
		"en": "Send right after git push: %s\n",
		"ko": "git push 직후 자동 전송: %s\n",
		"ja": "git push 直後の自動送信: %s\n",
	},
	"reposTitle": {
		"en": "Repositories",
		"ko": "저장소",
		"ja": "リポジトリ",
	},
	"settingsTitle": {
		"en": "Settings",
		"ko": "설정",
		"ja": "設定",
	},
	"syncTitle": {
		"en": "Sync",
		"ko": "동기화",
		"ja": "同期",
	},
	"autosyncAsk": {
		"en": "Send your commits to aline.team right after each git push? (off: only on sync or the daily sync)",
		"ko": "git push할 때마다 커밋을 aline.team에 바로 보낼까요? (끄면 sync·예약 동기화 때만 전송)",
		"ja": "git push のたびにコミットを aline.team へすぐ送信しますか?(オフ: sync・予約同期の時のみ送信)",
	},
	"yesNoAgain": {
		"en": "Please answer y or n.\n",
		"ko": "y 또는 n으로 답해 주세요.\n",
		"ja": "y または n で答えてください。\n",
	},
	"scheduleAsk": {
		"en": "Also sync once a day at a set time? It picks up pushes the hooks missed (other tools, --no-verify).",
		"ko": "매일 정해진 시각에도 동기화할까요? 훅이 놓친 push(다른 도구, --no-verify 등)를 보완합니다.",
		"ja": "毎日決まった時刻にも同期しますか?フックが拾えなかった push(他のツール、--no-verify など)を補います。",
	},
	"scheduleAskNew": {
		"en": " [HH:MM, e.g. 09:00 / Enter = no] > ",
		"ko": " [HH:MM, 예: 09:00 / Enter = 안 함] > ",
		"ja": " [HH:MM、例: 09:00 / Enter = しない] > ",
	},
	"scheduleAskKeep": {
		"en": " [HH:MM / off / Enter = keep %s] > ",
		"ko": " [HH:MM / off = 끄기 / Enter = %s 유지] > ",
		"ja": " [HH:MM / off = オフ / Enter = %s のまま] > ",
	},
	"scheduleFormat": {
		"en": "Enter a time like 09:00, or off.\n",
		"ko": "09:00처럼 시각을 입력하거나 off를 입력하세요.\n",
		"ja": "09:00 のように時刻を入力するか、off と入力してください。\n",
	},
	"scheduleOn": {
		"en": "Daily sync at %s is set up.\nIts output goes to: %s\n",
		"ko": "매일 %s에 동기화하도록 등록했습니다.\n실행 기록: %s\n",
		"ja": "毎日 %s に同期するよう登録しました。\n実行記録: %s\n",
	},
	"scheduleMacAccess": {
		"en": "macOS may ask once whether gitfolio can use folders such as Documents or Desktop.\nAllow it, or the daily sync cannot read repositories kept there.\n",
		"ko": "macOS가 gitfolio의 문서·데스크탑 등 폴더 접근을 한 번 물을 수 있습니다.\n허용해야 그 폴더에 있는 저장소를 예약 동기화가 읽을 수 있습니다.\n",
		"ja": "macOS が gitfolio による書類・デスクトップなどのフォルダーへのアクセスを一度確認することがあります。\n許可しないと、そこにあるリポジトリを予約同期が読み取れません。\n",
	},
	"scheduleOff": {
		"en": "The daily sync is removed.\n",
		"ko": "예약 동기화를 해제했습니다.\n",
		"ja": "予約同期を解除しました。\n",
	},
	"scheduleStatus": {
		"en": "Daily sync: %s\nLast sync: %s\n",
		"ko": "예약 동기화: %s\n마지막 동기화: %s\n",
		"ja": "予約同期: %s\n前回の同期: %s\n",
	},
	"scheduleUnsupported": {
		"en": "Scheduling is not supported on this system yet; run `gitfolio sync` yourself.\n",
		"ko": "이 시스템에서는 아직 예약 동기화를 지원하지 않습니다. `gitfolio sync`를 직접 실행하세요.\n",
		"ja": "このシステムでは予約同期にまだ対応していません。`gitfolio sync` を直接実行してください。\n",
	},
	"never": {
		"en": "never",
		"ko": "없음",
		"ja": "なし",
	},
	"lastSyncOK": {
		"en": "%s (done)",
		"ko": "%s (성공)",
		"ja": "%s (成功)",
	},
	"lastSyncFailed": {
		"en": "%s (failed: %s)",
		"ko": "%s (실패: %s)",
		"ja": "%s (失敗: %s)",
	},
	"depsUpdated": {
		"en": "%s: dependencies updated, %d commit(s) collected again.\n",
		"ko": "%s: 의존성을 갱신하고 커밋 %d개를 다시 수집했습니다.\n",
		"ja": "%s: 依存関係を更新し、コミット %d 件を再収集しました。\n",
	},
}
