package main

import (
	"fmt"
	"strings"
)

// detectLang picks the screen language from the environment (DESIGN 7.1): English by default,
// Korean or Japanese when the system language says so. The first variable that is set wins.
// ponytail: environment only; Windows needs an OS API call once Windows is supported.
func detectLang(getenv func(string) string) string {
	for _, k := range []string{"GITFOLIO_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.ToLower(strings.TrimSpace(getenv(k)))
		if v == "" {
			continue
		}
		switch {
		case strings.HasPrefix(v, "ko"):
			return "ko"
		case strings.HasPrefix(v, "ja"):
			return "ja"
		}
		return "en"
	}
	return "en"
}

// statusPrefix marks GitFolio's own result lines so they stand out among shell and git output.
// Prompts carry no prefix (it would blur where to type); errors stay "gitfolio: …" on stderr.
const statusPrefix = "== GitFolio == "

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

// say prints a translated result line with the GitFolio prefix; further lines are indented under it.
func say(lang, key string, args ...any) {
	under := strings.Repeat(" ", len(statusPrefix))
	for i, line := range strings.Split(strings.TrimRight(fmt.Sprintf(tr(lang, key), args...), "\n"), "\n") {
		if i == 0 {
			fmt.Println(margin + statusPrefix + line)
		} else {
			fmt.Println(margin + under + line)
		}
	}
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
   (masked), timestamps, file names, lines added/deleted,
   AI usage, and repository namespaces (owner/repo).
 - Files are read only with your approval, and only package
   manager files, only to detect dependencies.
 - Data is masked on this computer and sent to aline.team
   (Alineteam Inc., United States) after each git push and
   when you sync. Kept for 1 year from sign-up, renewed
   automatically while you keep using GitFolio.
 - An aline.team account is required.
 - Privacy policy: https://aline.team/privacy
`,
		"ko": ` [데이터 정책]
 - 소스 코드는 수집하지 않습니다.
 - 수집 항목: 커밋 해시, 작성자 이메일, 커밋 메시지(마스킹), 시점,
   파일명, 추가·삭제 줄 수, AI 사용 여부, 저장소 namespace(소유자/저장소)
 - 파일 읽기는 사용자가 승인한 패키지 매니저 파일에 한하며,
   의존성 파악에만 사용합니다.
 - 데이터는 이 컴퓨터에서 마스킹된 뒤, git push 직후와
   동기화할 때 aline.team(Alineteam Inc., 미국)으로 전송되며
   가입일로부터 1년간 보관됩니다. 계속 이용 중이면 자동 연장됩니다.
 - aline.team 가입이 필요합니다.
 - 개인정보처리방침: https://aline.team/privacy
`,
		"ja": ` [データポリシー]
 - ソースコードは収集しません。
 - 収集項目: コミットハッシュ、作成者のメールアドレス、
   コミットメッセージ(マスキング済み)、日時、ファイル名、
   追加・削除行数、AI の利用有無、リポジトリ(オーナー/リポジトリ)
 - ファイルの読み取りは、承認されたパッケージマネージャーの
   ファイルに限り、依存関係の把握にのみ使用します。
 - データはこのコンピューターでマスキングされた後、git push の
   直後と同期時に aline.team(Alineteam Inc.、米国)へ送信され、
   登録日から1年間保管されます。ご利用中は自動的に延長されます。
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
		"en": "\nYour commits are identified by: %s (git config)\nMore work emails can be added and verified once aline.team sign-in is available.\n",
		"ko": "\n본인 커밋은 이 이메일로 식별합니다: %s (git config)\n다른 작업 이메일은 aline.team 로그인이 준비되면 인증 후 추가할 수 있습니다.\n",
		"ja": "\nあなたのコミットはこのメールアドレスで識別します: %s (git config)\n他の業務用メールアドレスは、aline.team へのログイン対応後に認証して追加できます。\n",
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
		"en": "Turn on dependency detection? You will choose the files per repository. [y/N] > ",
		"ko": "의존성 분석을 켤까요? 읽을 파일은 저장소마다 직접 고릅니다. [y/N] > ",
		"ja": "依存関係の分析を有効にしますか? 読み取るファイルはリポジトリごとに選びます。[y/N] > ",
	},
	"noneSelected": {
		"en": "No repositories were chosen. Run `gitfolio init` again any time to choose.\n",
		"ko": "선택한 저장소가 없습니다. 언제든 `gitfolio init`을 다시 실행해 고를 수 있습니다.\n",
		"ja": "リポジトリは選択されませんでした。いつでも `gitfolio init` を再実行して選べます。\n",
	},
	"searching": {
		"en": "\nLooking for git repositories (folder names only; macOS may ask for folder access)...\n",
		"ko": "\ngit 저장소를 찾는 중입니다. (폴더 이름만 확인하며, macOS가 폴더 접근 권한을 물을 수 있습니다)\n",
		"ja": "\ngit リポジトリを検索しています。(フォルダー名のみ確認します。macOS がフォルダーへのアクセス許可を求める場合があります)\n",
	},
	"noCandidates": {
		"en": "No new repositories with your commits were found.\n",
		"ko": "본인 커밋이 있는 새 저장소를 찾지 못했습니다.\n",
		"ja": "あなたのコミットがある新しいリポジトリは見つかりませんでした。\n",
	},
	"companyNotice": {
		"en": "Repositories with your commits are listed above (commits, latest date).\nFrom the chosen ones, only masked commit messages, file names and repository names are sent, never source code.\nHide customer or internal project names with `gitfolio config mask add <word>`.\n",
		"ko": "위는 본인 커밋이 있는 저장소입니다. (커밋 수, 최근 커밋 날짜)\n선택한 저장소는 소스 코드 없이 커밋 메시지·파일명·저장소 이름만 마스킹해 전송합니다.\n고객사·사내 프로젝트명은 `gitfolio config mask add <단어>`로 가릴 수 있습니다.\n",
		"ja": "上記はあなたのコミットがあるリポジトリです。(コミット数、最新コミット日)\n選択したリポジトリからは、ソースコードではなく、マスキングしたコミットメッセージ・ファイル名・リポジトリ名のみを送信します。\n顧客名や社内プロジェクト名は `gitfolio config mask add <単語>` で隠せます。\n",
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
		"en": "\nDone. From now on, your pushes in the chosen repositories are collected automatically.\nRun `gitfolio init` again any time to add repositories.\n",
		"ko": "\n완료했습니다. 이제 선택한 저장소에서 push할 때마다 자동으로 수집됩니다.\n저장소를 추가하려면 언제든 `gitfolio init`을 다시 실행하세요.\n",
		"ja": "\n完了しました。今後、選択したリポジトリで push するたびに自動で収集されます。\nリポジトリを追加するには、いつでも `gitfolio init` を再実行してください。\n",
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
	"loginPending": {
		"en": "Sign-up and login to aline.team are coming soon; this step is skipped for now.\n",
		"ko": "aline.team 가입·로그인은 준비 중이라 이 단계는 지금 건너뜁니다.\n",
		"ja": "aline.team への登録・ログインは準備中のため、この手順は現在スキップします。\n",
	},
}
