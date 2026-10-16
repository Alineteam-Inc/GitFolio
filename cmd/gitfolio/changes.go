package main

import (
	"regexp"
	"slices"
	"strings"
)

// firstChanges finds, for each file the user created, the author date of the first later commit by
// someone else that changed it: aline.team's modifiedFiles. Files are followed by path, the
// way aline.team's own git sync does: a file renamed away is not followed, and someone else's rename is
// not a change to the created path. System commits (merge, release, ...) do not count. commits is in
// git log order, newest first; the result maps path → author date.
func firstChanges(commits []Commit, mine map[string]bool) map[string]string {
	created, first := map[string]bool{}, map[string]string{}
	for i := len(commits) - 1; i >= 0; i-- {
		c := commits[i]
		if creationType(c, mine) != "" {
			for _, f := range c.Files {
				if f.Created {
					created[f.Name] = true
				}
			}
			continue
		}
		if commitType(c.Message) == "SYSTEM" {
			continue
		}
		for _, f := range c.Files {
			if created[f.Name] && first[f.Name] == "" {
				first[f.Name] = c.Date
			}
		}
	}
	return first
}

// Commit types, aline.team's way: a Conventional Commits prefix, else the type of the highest ranked
// keyword in the subject (the message up to the first blank line). commitTypes is in rank order.
var commitTypes = []string{"HOTFIX", "FIX", "REVERT", "ADD", "PERF", "TEST", "REFACTOR", "CI", "ENV", "DOCUMENTATION", "CHORE", "SYSTEM", "WIP", "UNCATEGORIZED"}

var (
	conventionalPrefix = regexp.MustCompile(`^([a-zA-Z]+)(?:\([^)]+\))?!?:\s*`)
	conventionalTypes  = map[string]string{"feat": "ADD", "feature": "ADD", "fix": "FIX", "hotfix": "HOTFIX", "docs": "DOCUMENTATION",
		"doc": "DOCUMENTATION", "style": "CHORE", "refactor": "REFACTOR", "test": "TEST", "chore": "CHORE", "update": "CHORE",
		"build": "ENV", "ci": "CI", "perf": "PERF", "revert": "REVERT", "merge": "SYSTEM", "wip": "WIP"}
	// solverCommit is a problem-solving site's automatic commit ("Title: ..., Time: 56 ms, Memory: 31120 KB
	// -BaekjoonHub"): a new solution, whatever its statistics say.
	solverCommit   = regexp.MustCompile(`-(?:baekjoon|leet)hub\b|\btime:\s*\d+\s*ms,\s*memory:`)
	notWord        = regexp.MustCompile(`[^ㄱ-ㅎㅏ-ㅣ가-힣a-zA-Z0-9]+`)
	paragraphBreak = regexp.MustCompile(`[\n\v\f\r\x{85}\x{2028}\x{2029}]{2}`)
	systemFirst    = map[string]bool{"merge": true, "merged": true, "rebase": true, "rollback": true, "squash": true, "release": true, "cherry-pick": true, "cherrypick": true}
	// commitKeywords maps a subject word to its rank in commitTypes. A word in two lists keeps the later one.
	commitKeywords = func() map[string]int {
		m := map[string]int{}
		for _, g := range [][2]string{
			{"HOTFIX", "hotfix 핫픽스 핫패치 긴급 긴급수정 긴급패치 긴급배포 장애 장애복구 critical urgent emergency incident breaking patch " +
				"security 보안 취약점 exploit vulnerability rollback downtime outage prod production"},
			{"FIX", "fix fixed fixes bugfix bug 버그수정 오류수정 결함 error 에러 오류 버그 수정 수정사항 디버그 디버깅 debug debugging " +
				"resolve resolved resolves exception 예외 issue problem 문제 trouble crash leak timeout deadlock broken failing corruption " +
				"regression 회귀 incorrect wrong defect npe design 디자인"},
			{"ADD", "feat feature add added adds new create created creates implement implemented implements 추가 신규 생성 개발 구현 도입 " +
				"기능 기능추가 신기능 연동 구축 functionality enhancement 개선 introduce introducing launch 출시 마무리 완료 완성 finish complete " +
				"endpoint route controller service component hook middleware authentication authorization login signup session " +
				"setup module 모듈 plugin scaffold init project applicant api"},
			{"TEST", "test tests testing 테스트 테스트코드 테스트작성 테스트추가 unit unittest integration e2e spec specs coverage mock stub " +
				"단위테스트 통합테스트 테스트케이스 testcase scenario snapshot fixture factory tdd bdd assertion spy junit mockito validate " +
				"validation 검증 검사 verify verification"},
			{"REFACTOR", "refactor refactoring 리팩토링 리팩터링 리팩 restructure reorganize 재구성 재설계 cleanup 정리 코드정리 코드개선 개선작업 " +
				"improve improvement polish code quality extract rename move reorganization architecture legacy split separate 분리 " +
				"modularize decouple abstract generalize simplify streamline"},
			{"ENV", "env environment config configuration 설정파일 환경설정 환경변수 build deploy deployment 배포설정 dependencies dependency " +
				"의존성 설정 환경 빌드 배포 infrastructure infra 인프라 devops properties yaml yml gradle maven pom container 컨테이너 compose " +
				"containerize orchestration provisioning automation"},
			{"DOCUMENTATION", "docs doc documentation 문서화 문서추가 문서업데이트 주석추가 readme comment comments 문서 주석 설명 guide manual " +
				"tutorial wiki changelog license annotation annotations markdown specification example javadoc swagger openapi api문서"},
			{"CHORE", "chore style format formatting remove removed delete deleted clean typo 오타 삭제 제거 변경 update updated 업데이트 change " +
				"changed modify modified bump version 버전 maintenance maintain upgrade downgrade lint linting ignore workspace editor unused " +
				"미사용 deprecated obsolete 불필요한 housekeeping tidy"},
			{"SYSTEM", "merged squashed initial automated auto bot"},
			{"WIP", "wip 작업중 진행중 개발중 임시 temp temporary draft checkpoint backup ongoing 진행 incomplete partial halfway 미완성 " +
				"prototype spike experiment research todo fixme poc 보류 pending"},
			{"PERF", "perf performance optimize optimization 최적화 성능 성능개선 성능최적화 cache 캐시 캐싱 throughput latency benchmark " +
				"tuning 튜닝 speed query 쿼리 쿼리최적화 index 인덱스 인덱싱 memory 메모리 lazy pagination 페이지네이션 parallel 병렬 병렬처리 " +
				"compress 압축"},
			{"REVERT", "revert reverted undo 되돌리기 되돌림 롤백해제 원복 취소 복구 restore"},
			{"CI", "ci cd cicd pipeline workflow actions action github jenkins travis circleci dockerfile docker gitlab kubernetes k8s helm " +
				"자동배포 배포자동화 자동화 배포파이프라인 빌드자동화"},
		} {
			for _, w := range strings.Fields(g[1]) {
				m[w] = slices.Index(commitTypes, g[0])
			}
		}
		return m
	}()
)

func commitType(message string) string {
	subject := strings.ToLower(paragraphBreak.Split(strings.ReplaceAll(message, "\r\n", "\n"), 2)[0])
	if strings.TrimSpace(subject) == "" {
		return "UNCATEGORIZED"
	}
	if m := conventionalPrefix.FindStringSubmatch(subject); m != nil && conventionalTypes[m[1]] != "" {
		return conventionalTypes[m[1]]
	}
	if solverCommit.MatchString(subject) {
		return "ADD"
	}
	words := strings.Fields(notWord.ReplaceAllString(subject, " "))
	if len(words) == 0 {
		return "UNCATEGORIZED"
	}
	if systemFirst[words[0]] {
		return "SYSTEM"
	}
	rank := len(commitTypes) - 1
	for _, w := range words {
		if r, ok := commitKeywords[w]; ok && r < rank {
			if rank = r; rank == 0 {
				break
			}
		}
	}
	return commitTypes[rank]
}
