package main

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// detectLang picks the screen language (DESIGN 7.1): GITFOLIO_LANG if set, else the device's language
// (on macOS the first one GitFolio has in System Settings › Language & Region, because terminals often
// set LANG to en_US whatever the device language is; on Windows the display language), else LC_ALL,
// LC_MESSAGES or LANG, else English.
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

// osLanguages returns the device's preferred languages, most preferred first, read once per run
// (deviceLanguages is per OS: platform_unix.go, platform_windows.go).
var osLanguages = sync.OnceValue(deviceLanguages)

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
// Questions start with "? " (see prompt); notices carry only the margin.
const statusPrefix = "GitFolio >> "

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

// warn is say for problems, on stderr. A problem always starts its own paragraph, so it stands out.
func warn(lang, key string, args ...any) {
	inParagraph = false
	show(os.Stderr, fmt.Sprintf(tr(lang, key), args...))
	inParagraph = false
}

// inParagraph is true while the last thing printed was a status line: the next one continues that
// paragraph under its text instead of repeating the prefix. A blank line, a notice, a question or a
// section header ends the paragraph (see blank, notice, prompt, section).
var inParagraph bool

// show prints text with the GitFolio prefix on its first line, unless it continues a paragraph, and
// the other lines aligned under it.
func show(w io.Writer, text string) {
	under := strings.Repeat(" ", len(statusPrefix))
	for i, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if i == 0 && !inParagraph {
			fmt.Fprintln(w, margin+statusPrefix+line)
		} else {
			fmt.Fprintln(w, margin+under+line)
		}
	}
	inParagraph = true
}

// notice prints an explanation with the margin; it ends a status paragraph.
func notice(s string) {
	inParagraph = false
	fmt.Print(indent(s))
}

// blank prints an empty line between paragraphs.
func blank() {
	inParagraph = false
	fmt.Println()
}

// failure returns a translated error; main prints it with the GitFolio prefix. Errors that come from
// git itself or only show up in hooks stay as they are.
func failure(key string, args ...any) error {
	return errors.New(strings.TrimSpace(fmt.Sprintf(tr(detectLang(os.Getenv), key), args...)))
}

// section prints the separator that opens an interactive command.
func section(lang, titleKey string) {
	inParagraph = false
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
`,
		"ko": ` [데이터 정책]
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
`,
		"ja": ` [データポリシー]
 - ソースコードは収集しません。
 - 収集項目: コミットハッシュ、作成者のメールアドレス、
   コミットメッセージ(マスキング済み)、ブランチ名、日時、リポジトリ内のファイルパス、
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
		"en": "Press Enter to continue, or Ctrl+C to quit. ",
		"ko": "계속하려면 Enter, 중단하려면 Ctrl+C ",
		"ja": "続行するには Enter、中止するには Ctrl+C ",
	},
	"rootsAsk": {
		"en": "Which folders hold your repositories? (comma-separated)\n",
		"ko": "저장소를 모아 둔 폴더를 입력하세요. (여러 개는 쉼표로 구분)\n",
		"ja": "リポジトリを置いているフォルダーを入力してください。(複数はカンマ区切り)\n",
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
	"identityWork": {
		"en": "Your commits are the ones by these work emails (and each repository's git email);\naline.team gets them all under the primary email ★.\n",
		"ko": "아래 작업 이메일(과 저장소별 git 이메일)로 쓴 커밋을 본인 커밋으로 모으고,\naline.team에는 모두 ★ 대표 이메일로 보냅니다.\n",
		"ja": "以下の作業用メールアドレス(とリポジトリごとの git のメールアドレス)のコミットをあなたのコミットとして集め、\naline.team にはすべて ★ 代表メールアドレスで送信します。\n",
	},
	"emailMoreAsk": {
		"en": "Any other work emails? Their commits count as yours too.\n[comma-separated / Enter = none] > ",
		"ko": "다른 작업 이메일이 있나요? 그 이메일로 쓴 커밋도 본인 커밋으로 모읍니다.\n[쉼표로 구분 / Enter = 없음] > ",
		"ja": "他の作業用メールアドレスはありますか?そのアドレスのコミットもあなたのコミットとして集めます。\n[カンマ区切り / Enter = なし] > ",
	},
	"emailList": {
		"en": "Work emails (★ primary)\n",
		"ko": "작업 이메일 (★ 대표)\n",
		"ja": "作業用メールアドレス (★ 代表)\n",
	},
	"emailNone": {
		"en": "No work emails yet. Add one with `gitfolio email add <email>`.\n",
		"ko": "등록한 작업 이메일이 없습니다. `gitfolio email add <이메일>`로 추가하세요.\n",
		"ja": "作業用メールアドレスはまだありません。`gitfolio email add <メールアドレス>` で追加してください。\n",
	},
	"emailAdded": {
		"en": "Added. The next scan also collects their commits.\n",
		"ko": "추가했습니다. 다음 수집부터 이 이메일의 커밋도 모읍니다.\n",
		"ja": "追加しました。次の収集からこのアドレスのコミットも集めます。\n",
	},
	"emailRemoved": {
		"en": "Removed. Commits already collected stay.\n",
		"ko": "삭제했습니다. 이미 모은 커밋은 그대로 둡니다.\n",
		"ja": "削除しました。すでに集めたコミットはそのままです。\n",
	},
	"emailPrimary": {
		"en": "Primary email is now %s.\nRepositories already on aline.team stay under the email they started with; new ones use this one.\n",
		"ko": "대표 이메일을 %s(으)로 바꿨습니다.\naline.team에 이미 있는 저장소는 처음 이메일로 그대로 집계되고, 새 저장소부터 이 이메일을 씁니다.\n",
		"ja": "代表メールアドレスを %s に変更しました。\naline.team にすでにあるリポジトリは最初のアドレスのまま集計され、新しいリポジトリからこのアドレスを使います。\n",
	},
	"emailUnverified": {
		"en": "✓ = verified. aline.team takes commits only under verified emails, and merges the repositories\nyou linked on the web under them; verify one with `gitfolio email verify <email>`.\n",
		"ko": "✓ = 인증됨. aline.team은 인증된 이메일의 커밋만 받고, 웹에서 그 이메일로 연동한 저장소와 하나로 합칩니다.\n`gitfolio email verify <이메일>`로 인증하세요.\n",
		"ja": "✓ = 認証済み。aline.team は認証済みのアドレスのコミットのみを受け付け、Web でそのアドレスで連携したリポジトリと\n1 つにまとめます。`gitfolio email verify <メールアドレス>` で認証してください。\n",
	},
	"emailVerified": {
		"en": "%s is verified.\n",
		"ko": "%s을(를) 인증했습니다.\n",
		"ja": "%s を認証しました。\n",
	},
	"emailVerifyFailed": {
		"en": "Could not verify %s (%v). It stays in the list; try again with `gitfolio email verify`.\n",
		"ko": "%s을(를) 인증하지 못했습니다(%v). 목록에는 그대로 있으니 `gitfolio email verify`로 다시 시도하세요.\n",
		"ja": "%s を認証できませんでした(%v)。一覧には残ります。`gitfolio email verify` でもう一度お試しください。\n",
	},
	"emailTooMany": {
		"en": "aline.team takes at most 20 verified work emails. Remove one first: gitfolio email rm <email>\n",
		"ko": "aline.team은 인증된 작업 이메일을 20개까지 받습니다. 먼저 하나를 삭제하세요: gitfolio email rm <이메일>\n",
		"ja": "aline.team が受け付ける認証済みの作業用メールアドレスは 20 件までです。先に 1 件削除してください: gitfolio email rm <メールアドレス>\n",
	},
	"emailVerifyNeedsTerminal": {
		"en": "Verifying an email needs a terminal: run `gitfolio email verify <email>` there.\n",
		"ko": "이메일 인증은 터미널에서 해야 합니다. 터미널에서 `gitfolio email verify <이메일>`을 실행하세요.\n",
		"ja": "メールアドレスの認証にはターミナルが必要です。ターミナルで `gitfolio email verify <メールアドレス>` を実行してください。\n",
	},
	"emailUnverifyFailed": {
		"en": "Removed here, but aline.team still has %s as verified (%v).\n",
		"ko": "이 컴퓨터에서는 삭제했지만 aline.team에는 %s이(가) 인증된 이메일로 남아 있습니다(%v).\n",
		"ja": "このコンピューターからは削除しましたが、aline.team には %s が認証済みとして残っています(%v)。\n",
	},
	"codeAskSkip": {
		"en": "Enter the code sent to %s (Enter = later) > ",
		"ko": "%s(으)로 보낸 확인 코드를 입력하세요 (Enter = 나중에) > ",
		"ja": "%s に送信した確認コードを入力してください (Enter = 後で) > ",
	},
	"emailSkipped": {
		"en": "Skipped %s. Verify it later with `gitfolio email verify`.\n",
		"ko": "%s 인증을 건너뛰었습니다. 나중에 `gitfolio email verify`로 인증하세요.\n",
		"ja": "%s の認証をスキップしました。後で `gitfolio email verify` で認証してください。\n",
	},
	"emailNoreply": {
		"en": "%s is a noreply address: it cannot receive a code, and aline.team needs none for it.\n",
		"ko": "%s은(는) noreply 주소라 코드를 받을 수 없고, aline.team도 인증을 요구하지 않습니다.\n",
		"ja": "%s は noreply アドレスのためコードを受け取れず、aline.team も認証を求めません。\n",
	},
	"emailCandidates": {
		"en": "Emails git uses for commits in these folders (repositories using each):\n",
		"ko": "이 폴더의 저장소에서 git이 커밋에 쓰는 이메일 (쓰는 저장소 수):\n",
		"ja": "これらのフォルダーのリポジトリで git がコミットに使うメールアドレス (使っているリポジトリ数):\n",
	},
	"emailRepoCount": {
		"en": "%d repo(s)",
		"ko": "저장소 %d개",
		"ja": "%d 件",
	},
	"emailSelect": {
		"en": "Which of these are your emails? Commits by them count as yours, and each is verified with a code.\n[all / 1,3 / Enter = all] > ",
		"ko": "이 중 본인 이메일은 무엇인가요? 그 이메일로 쓴 커밋을 본인 커밋으로 모으고, 이메일마다 코드로 인증합니다.\n[all / 1,3 / Enter = 전부] > ",
		"ja": "このうちあなたのメールアドレスはどれですか? そのアドレスのコミットをあなたのコミットとして集め、アドレスごとにコードで認証します。\n[all / 1,3 / Enter = すべて] > ",
	},
	"emailVerifyNotice": {
		"en": "aline.team takes commits only under emails verified for your account. A code goes to each email\nbelow, one at a time (Enter skips one; verify it later with `gitfolio email verify`).\n",
		"ko": "aline.team은 계정에 인증된 이메일의 커밋만 받습니다. 아래 이메일마다 차례로 코드를 보냅니다.\n(Enter = 건너뛰기, 나중에 `gitfolio email verify`로 인증)\n",
		"ja": "aline.team はアカウントで認証済みのメールアドレスのコミットのみを受け付けます。以下のアドレスに順にコードを送ります。\n(Enter = スキップ、後で `gitfolio email verify` で認証)\n",
	},
	"emailCheckOnce": {
		"en": "Since GitFolio 0.2.0, aline.team takes commits only under emails verified for your account\n(GitHub and GitLab noreply addresses need none). Not verified yet: %s\n",
		"ko": "GitFolio 0.2.0부터 aline.team은 계정에 인증된 이메일의 커밋만 받습니다.\n(GitHub·GitLab noreply 주소는 인증 불필요) 아직 인증하지 않은 이메일: %s\n",
		"ja": "GitFolio 0.2.0 から、aline.team はアカウントで認証済みのメールアドレスのコミットのみを受け付けます。\n(GitHub・GitLab の noreply アドレスは不要) まだ認証していないアドレス: %s\n",
	},
	"emailVerifyNow": {
		"en": "Verify them now with an email code? [Y/n] > ",
		"ko": "지금 이메일 코드로 인증할까요? [Y/n] > ",
		"ja": "今メールのコードで認証しますか? [Y/n] > ",
	},
	"sendWaiting": {
		"en": "Not sent yet: aline.team takes these repositories only after %s is verified for your account.\n  %s\nVerify it with `gitfolio email verify %s`; the next push or sync sends them.\n",
		"ko": "아직 보내지 않았습니다: %s을(를) 계정에 인증해야 aline.team이 아래 저장소를 받습니다.\n  %s\n`gitfolio email verify %s`로 인증하면 다음 push나 sync 때 보냅니다.\n",
		"ja": "まだ送信していません: %s をアカウントで認証すると、aline.team が以下のリポジトリを受け付けます。\n  %s\n`gitfolio email verify %s` で認証すると、次の push か sync で送信します。\n",
	},
	"purgeQueued": {
		"en": "Its commits were deleted here. The next sync deletes the repository and your data in it on aline.team;\nif you also linked it on the web, that link and the pull request events you took part in go too.\n",
		"ko": "이 컴퓨터의 커밋은 지웠습니다. 다음 동기화 때 aline.team에서도 이 저장소와 내 데이터를 지웁니다.\n웹에서도 연동했다면 그 연동과 내가 참여한 PR 기록도 함께 지워집니다.\n",
		"ja": "このコンピューターのコミットは削除しました。次の同期で aline.team からもこのリポジトリとあなたのデータを削除します。\nWeb でも連携していた場合、その連携とあなたが参加したプルリクエストの記録も削除されます。\n",
	},
	"emailBad": {
		"en": "Not a valid email address: %s\n",
		"ko": "올바른 이메일 주소가 아닙니다: %s\n",
		"ja": "正しいメールアドレスではありません: %s\n",
	},
	"emailNotFound": {
		"en": "Not one of your work emails: %s\n",
		"ko": "등록된 작업 이메일이 아닙니다: %s\n",
		"ja": "登録された作業用メールアドレスではありません: %s\n",
	},
	"emailRmPrimary": {
		"en": "%s is the primary email. Make another one primary first: gitfolio email primary <email>\n",
		"ko": "대표 이메일은 지울 수 없습니다: %s\n먼저 다른 이메일을 대표로 지정하세요: gitfolio email primary <이메일>\n",
		"ja": "代表メールアドレスは削除できません: %s\n先に別のアドレスを代表に指定してください: gitfolio email primary <メールアドレス>\n",
	},
	"identityMissing": {
		"en": "\ngit user.email is not set, so your commits cannot be identified.\nSet it with: git config --global user.email you@example.com\n",
		"ko": "\ngit user.email이 설정되어 있지 않아 본인 커밋을 식별할 수 없습니다.\n설정 방법: git config --global user.email you@example.com\n",
		"ja": "\ngit の user.email が設定されていないため、あなたのコミットを識別できません。\n設定方法: git config --global user.email you@example.com\n",
	},
	"depsNotice": {
		"en": "Dependency detection reads the package manager files you select (package.json, go.mod,\npom.xml, build.gradle, ...) in each repository, only to detect dependencies.\nOnly dependency names and versions are kept; file contents and paths are never stored or sent.\nThey are read only when you ask: right after you approve them, with `gitfolio deps scan`,\nor by scan, sync and the daily sync if you turn that on (`gitfolio deps auto on`). Pushes never read them.\n",
		"ko": "의존성 분석은 저장소마다 사용자가 고른 패키지 매니저 파일(package.json, go.mod,\npom.xml, build.gradle 등)만, 의존성 파악 용도로만 읽습니다.\n의존성 이름과 버전만 남기며, 파일 원문과 경로는 저장하지도 전송하지도 않습니다.\n읽는 때는 요청할 때뿐입니다: 승인한 직후, `gitfolio deps scan` 실행 시, 켜 두었다면 scan·sync·예약 동기화 때\n(`gitfolio deps auto on`). push 때는 읽지 않습니다.\n",
		"ja": "依存関係の分析では、リポジトリごとにあなたが選んだパッケージマネージャーのファイル\n(package.json、go.mod、pom.xml、build.gradle など)のみを、依存関係の把握のためだけに読み取ります。\n依存関係の名前とバージョンのみを保持し、ファイルの内容とパスは保存も送信もしません。\n読むのは求められたときだけです: 承認した直後、`gitfolio deps scan` の実行時、オンにした場合は scan・sync・予約同期のとき\n(`gitfolio deps auto on`)。push のときは読みません。\n",
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
		"en": "Looking for git repositories (folder names only)...\n",
		"ko": "git 저장소를 찾는 중입니다. (폴더 이름만 확인합니다)\n",
		"ja": "git リポジトリを検索しています。(フォルダー名のみ確認します)\n",
	},
	"searchingMac": {
		"en": "macOS may ask for access to folders such as Documents.\n",
		"ko": "macOS가 문서 등 폴더 접근 권한을 물을 수 있습니다.\n",
		"ja": "macOS が書類などのフォルダーへのアクセス許可を求める場合があります。\n",
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
		"en": "Setup is done.\nRepositories: %d\nPrimary email: %s\nSend right after git push: %s\nDependency detection: %s\nDaily sync: %s\nChange these any time by running `gitfolio init` again, or with `gitfolio config`, `email`, `deps` and `schedule`.\n",
		"ko": "설정을 마쳤습니다.\n등록 저장소: %d개\n대표 이메일: %s\npush 직후 자동 전송: %s\n의존성 분석: %s\n예약 동기화: %s\n언제든 `gitfolio init`을 다시 실행하거나 `gitfolio config`·`email`·`deps`·`schedule`로 바꿀 수 있습니다.\n",
		"ja": "設定が完了しました。\n登録リポジトリ: %d 件\n代表メールアドレス: %s\ngit push 直後の自動送信: %s\n依存関係の分析: %s\n予約同期: %s\nいつでも `gitfolio init` を再実行するか、`gitfolio config`・`email`・`deps`・`schedule` で変更できます。\n",
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
		"en": "Logging in or signing up needs a terminal: run `gitfolio login` there.\n",
		"ko": "로그인·가입은 터미널에서 해야 합니다. 터미널에서 `gitfolio login`을 실행하세요.\n",
		"ja": "ログイン・新規登録にはターミナルが必要です。ターミナルで `gitfolio login` を実行してください。\n",
	},
	"loginTitle": {
		"en": "Log in / Sign up",
		"ko": "로그인 / 가입",
		"ja": "ログイン / 新規登録",
	},
	"notLoggedIn": {
		"en": "Not logged in. Run `gitfolio login` to log in or sign up.\n",
		"ko": "로그인되어 있지 않습니다. `gitfolio login`으로 로그인하거나 가입하세요.\n",
		"ja": "ログインしていません。`gitfolio login` でログインまたは新規登録してください。\n",
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
		"en": "Log in or sign up to aline.team first: gitfolio login\n",
		"ko": "먼저 aline.team에 로그인하거나 가입하세요: gitfolio login\n",
		"ja": "先に aline.team にログインまたは新規登録してください: gitfolio login\n",
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
		"en": "Hooks live outside .git (%s, e.g. a shared core.hooksPath or husky 5–8), so they were not changed.\nAdd these lines yourself:\n  pre-push:    gitfolio hook pre-push \"$PPID\"\n  post-commit: gitfolio hook post-commit\n",
		"ko": "훅 폴더가 .git 밖에 있어(%s, 예: 공유 core.hooksPath·husky 5–8) 훅을 바꾸지 않았습니다.\n아래 줄을 직접 추가하세요:\n  pre-push:    gitfolio hook pre-push \"$PPID\"\n  post-commit: gitfolio hook post-commit\n",
		"ja": "フックが .git の外にあるため(%s、例: 共有の core.hooksPath・husky 5–8)、変更していません。\n次の行をご自身で追加してください:\n  pre-push:    gitfolio hook pre-push \"$PPID\"\n  post-commit: gitfolio hook post-commit\n",
	},
	"updateAsk": {
		"en": "GitFolio %s is available (you have %s). Update now? [Y/n] > ",
		"ko": "GitFolio %s 버전이 나왔습니다 (지금 %s). 지금 업데이트할까요? [Y/n] > ",
		"ja": "GitFolio %s が公開されています(現在 %s)。今すぐ更新しますか? [Y/n] > ",
	},
	"updated": {
		"en": "Updated GitFolio to %s. Run the command again.\n",
		"ko": "GitFolio를 %s(으)로 업데이트했습니다. 명령을 다시 실행하세요.\n",
		"ja": "GitFolio を %s に更新しました。もう一度コマンドを実行してください。\n",
	},
	"updateFailed": {
		"en": "Could not update (%v). Update by hand:\n  %s\n",
		"ko": "업데이트하지 못했습니다(%v). 직접 업데이트하세요:\n  %s\n",
		"ja": "更新できませんでした(%v)。手動で更新してください:\n  %s\n",
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
		"en": "Git hooks were not installed: %v\nPushes from this repository are not sent right away; `gitfolio sync` or the daily sync (`gitfolio schedule`) sends them.\n",
		"ko": "git hook을 설치하지 못했습니다: %v\n이 저장소는 push 직후 자동 전송되지 않습니다. `gitfolio sync`나 예약 동기화(`gitfolio schedule`)로 보냅니다.\n",
		"ja": "git フックをインストールできませんでした: %v\nこのリポジトリは git push 直後には送信されません。`gitfolio sync` か毎日の同期(`gitfolio schedule`)で送信します。\n",
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
		"en": "Package manager files. Selected files are read only to detect dependencies:\n",
		"ko": "패키지 매니저 파일입니다. 고른 파일은 의존성 파악에만 읽습니다.\n",
		"ja": "パッケージマネージャーのファイルです。選んだファイルは依存関係の把握にのみ読み取ります。\n",
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
	"historyOff": {
		"en": "The git command history is off. Turn it on: gitfolio config git-history on\n",
		"ko": "git 명령 기록이 꺼져 있습니다. 켜기: gitfolio config git-history on\n",
		"ja": "git コマンド履歴はオフです。オンにする: gitfolio config git-history on\n",
	},
	"historyEmpty": {
		"en": "No git commands recorded yet.\n",
		"ko": "아직 기록된 git 명령이 없습니다.\n",
		"ja": "記録された git コマンドはまだありません。\n",
	},
	"historyShown": {
		"en": "Showing the last %[1]d of %[2]d runs (--all: every run). git writes this record itself (GIT_TRACE): %[3]s, up to %[4]s, oldest runs removed first.\n",
		"ko": "실행 %[2]d회 중 최근 %[1]d회를 보여 줍니다 (--all: 전부). 이 기록은 git이 직접 씁니다(GIT_TRACE): %[3]s, 최대 %[4]s, 오래된 실행부터 지웁니다.\n",
		"ja": "実行 %[2]d 回のうち直近 %[1]d 回を表示しています(--all: すべて)。この記録は git 自身が書き込みます(GIT_TRACE): %[3]s、最大 %[4]s、古い実行から削除します。\n",
	},
	"historyTurnedOn": {
		"en": "git command history: on. See it with gitfolio history.\n",
		"ko": "git 명령 기록: 켬. gitfolio history로 볼 수 있습니다.\n",
		"ja": "git コマンド履歴: オン。gitfolio history で確認できます。\n",
	},
	"historyTurnedOff": {
		"en": "git command history: off. The record was deleted.\n",
		"ko": "git 명령 기록: 끔. 기록을 삭제했습니다.\n",
		"ja": "git コマンド履歴: オフ。記録を削除しました。\n",
	},
	"historySize": {
		"en": "git command history keeps up to %s; the oldest runs are removed first.\n",
		"ko": "git 명령 기록은 최대 %s까지 보관하고, 오래된 실행부터 지웁니다.\n",
		"ja": "git コマンド履歴は最大 %s まで保存し、古い実行から削除します。\n",
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
	"yesNoAgain": {
		"en": "Please answer y or n.\n",
		"ko": "y 또는 n으로 답해 주세요.\n",
		"ja": "y または n で答えてください。\n",
	},
	"scheduleAsk": {
		"en": "Collect and send automatically every day at a set time?\n(without it, run `gitfolio sync` yourself; pushed commits are still sent right away)",
		"ko": "매일 정해진 시각에 자동으로 수집·전송할까요?\n(예약하지 않으면 `gitfolio sync`로 직접 실행, push한 커밋은 지금처럼 바로 전송)",
		"ja": "毎日決まった時刻に自動で収集・送信しますか?\n(予約しない場合は `gitfolio sync` を直接実行。push したコミットはこれまでどおりすぐ送信)",
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
	"depsAuto": {
		"en": "scan, sync and the daily sync read the approved package manager files too: %s\n",
		"ko": "scan·sync·예약 동기화 때 승인한 패키지 매니저 파일도 읽기: %s\n",
		"ja": "scan・sync・予約同期で承認したパッケージマネージャーのファイルも読む: %s\n",
	},
	"depsAutoStatus": {
		"en": "Read with scan, sync and the daily sync: %s (gitfolio deps auto on|off)\n",
		"ko": "scan·sync·예약 동기화 때 읽기: %s (gitfolio deps auto on|off)\n",
		"ja": "scan・sync・予約同期で読む: %s (gitfolio deps auto on|off)\n",
	},
	"depsScanned": {
		"en": "%s: read %d approved package manager file(s), %d dependencies.\n",
		"ko": "%s: 승인한 패키지 매니저 파일 %d개를 읽어 의존성 %d개를 확인했습니다.\n",
		"ja": "%s: 承認したパッケージマネージャーのファイル %d 件を読み、依存関係 %d 件を確認しました。\n",
	},
	"scheduleNoScheduler": {
		"en": "There is no scheduler to use: systemd user timers failed (%v) and crontab is not installed.\nInstall cron, or run `gitfolio sync` yourself.\n",
		"ko": "쓸 수 있는 예약 도구가 없습니다: systemd 사용자 타이머 실패(%v), crontab 없음.\ncron을 설치하거나 `gitfolio sync`를 직접 실행하세요.\n",
		"ja": "使える予約ツールがありません: systemd ユーザータイマーが失敗(%v)し、crontab もありません。\ncron をインストールするか、`gitfolio sync` を直接実行してください。\n",
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
