package hub

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	CircleSkillExtensionURI          = "https://github.com/tbdavid2019/888a2a-lite/extensions/circle-skills/v1"
	MaxCircleSkillBytes              = 32 * 1024
	MaxCircleSkillsPerCircle         = 32
	MaxCircleSkillTotalBytes         = 256 * 1024
	MaxCircleSkillProposalsPerCircle = 16
	MaxCircleSkillRevisionsPerSlug   = 20
	MaxCircleSkillTTLSeconds         = int64(365 * 24 * 3600)
	MinCircleSkillTTLSeconds         = int64(60)
	MaxCircleSkillTitleLength        = 128
	MaxCircleSkillSummaryLength      = 280
)

const (
	SkillProposalStatusPending  = "PENDING_REVIEW"
	SkillProposalStatusApproved = "APPROVED"
	SkillProposalStatusRejected = "REJECTED"
)

const (
	SkillEventActionUpdated         = "updated"
	SkillEventActionDeleted         = "deleted"
	SkillEventActionExpired         = "expired"
	SkillEventActionProposalCreated = "proposal_created"
	SkillEventActionProposalUpdated = "proposal_updated"
	SkillEventActionProposalRejected = "proposal_rejected"
)

var (
	circleSkillSlugPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	skillRealSecretPattern     = regexp.MustCompile(`(?i)(-----BEGIN\s+(?:RSA|EC|OPENSSH|DSA|PGP)?\s*PRIVATE\s+KEY-----|sk-[A-Za-z0-9_-]{20,}|ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|xox[baprs]-[A-Za-z0-9-]{10,}|re_[A-Za-z0-9_]{20,}|AKIA[0-9A-Z]{16})`)
	skillPlaceholderValue      = regexp.MustCompile(`^(?:<[^>]+>|\$\{[^}]+\}|\{\{[^}]+\}\}|YOUR_[A-Z0-9_]+|EXAMPLE[_A-Z0-9]*|REDACTED|CHANGEME|\.\.\.)$`)
	skillInlineTargetPattern   = regexp.MustCompile(`!?\[[^\]]*\]\(\s*([^)\s]+)(?:\s+["'][^"']*["'])?\s*\)`)
	skillAngleLinkPattern      = regexp.MustCompile(`<\s*([a-zA-Z][a-zA-Z0-9+.-]*:[^\s>]+)\s*>`)
	skillRefDefinitionPattern  = regexp.MustCompile(`(?m)^\s*\[[^\]]+\]:\s*(\S+)`)
	skillUnsafeSchemePattern   = regexp.MustCompile(`(?i)^(?:javascript|vbscript|data|file|blob):`)
	skillYAMLTagOrAliasPattern = regexp.MustCompile(`(?:!!|&\w+|\*\w+)`)
)

type CircleSkill struct {
	HubID       string  `json:"hubId,omitempty"`
	CircleID    string  `json:"circleId,omitempty"`
	Slug        string  `json:"slug"`
	Title       string  `json:"title"`
	Summary     string  `json:"summary"`
	Content     string  `json:"content,omitempty"`
	Version     int64   `json:"version"`
	ContentHash string  `json:"contentHash"`
	ByteSize    int     `json:"byteSize"`
	TTLSeconds  *int64  `json:"ttlSeconds,omitempty"`
	ExpiresAt   *string `json:"expiresAt,omitempty"`
	ProposedBy  string  `json:"proposedBy,omitempty"`
	ApprovedBy  string  `json:"approvedBy,omitempty"`
	UpdatedBy   string  `json:"updatedBy,omitempty"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
	DeletedAt   *string `json:"deletedAt,omitempty"`
}

type CircleSkillManifestItem struct {
	Slug        string  `json:"slug"`
	Title       string  `json:"title"`
	Summary     string  `json:"summary"`
	Version     int64   `json:"version"`
	ContentHash string  `json:"contentHash"`
	ByteSize    int     `json:"byteSize"`
	TTLSeconds  *int64  `json:"ttlSeconds,omitempty"`
	ExpiresAt   *string `json:"expiresAt,omitempty"`
	ProposedBy  string  `json:"proposedBy,omitempty"`
	ApprovedBy  string  `json:"approvedBy,omitempty"`
	UpdatedBy   string  `json:"updatedBy,omitempty"`
	UpdatedAt   string  `json:"updatedAt"`
}

type CircleSkillManifest struct {
	HubID             string                    `json:"hubId,omitempty"`
	CircleID          string                    `json:"circleId"`
	CollectionVersion int64                     `json:"collectionVersion"`
	SkillCount        int                       `json:"skillCount"`
	ETag              string                    `json:"etag"`
	Skills            []CircleSkillManifestItem `json:"skills"`
}

type CircleSkillProposal struct {
	HubID               string  `json:"hubId,omitempty"`
	CircleID            string  `json:"circleId,omitempty"`
	Slug                string  `json:"slug"`
	BaseVersion         int64   `json:"baseVersion"`
	Title               string  `json:"title"`
	Summary             string  `json:"summary"`
	Content             string  `json:"content,omitempty"`
	ContentHash         string  `json:"contentHash"`
	ByteSize            int     `json:"byteSize"`
	RequestedTTLSeconds *int64  `json:"requestedTtlSeconds,omitempty"`
	ProposedBy          string  `json:"proposedBy"`
	Status              string  `json:"status"`
	RejectionReason     string  `json:"rejectionReason,omitempty"`
	CreatedAt           string  `json:"createdAt"`
	UpdatedAt           string  `json:"updatedAt"`
	ReviewedBy          string  `json:"reviewedBy,omitempty"`
	ReviewedAt          *string `json:"reviewedAt,omitempty"`
}

type CircleSkillRevision struct {
	HubID       string  `json:"hubId,omitempty"`
	CircleID    string  `json:"circleId,omitempty"`
	Slug        string  `json:"slug"`
	Version     int64   `json:"version"`
	ContentHash string  `json:"contentHash"`
	ByteSize    int     `json:"byteSize"`
	Content     string  `json:"content,omitempty"`
	Action      string  `json:"action"`
	ExpiresAt   *string `json:"expiresAt,omitempty"`
	UpdatedBy   string  `json:"updatedBy,omitempty"`
	CreatedAt   string  `json:"createdAt"`
}

type CircleSkillInput struct {
	Title           string `json:"title,omitempty"`
	Summary         string `json:"summary,omitempty"`
	Content         string `json:"content"`
	TTLSeconds      *int64 `json:"ttlSeconds,omitempty"`
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

type CircleSkillProposalInput struct {
	Title               string `json:"title,omitempty"`
	Summary             string `json:"summary,omitempty"`
	Content             string `json:"content"`
	BaseVersion         int64  `json:"baseVersion"`
	RequestedTTLSeconds *int64 `json:"requestedTtlSeconds,omitempty"`
	IdempotencyKey      string `json:"idempotencyKey"`
}

type CircleSkillDecisionInput struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	TTLSeconds      *int64 `json:"ttlSeconds,omitempty"`
	Reason          string `json:"reason,omitempty"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

type CircleSkillDeleteInput struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

type CircleSkillEvent struct {
	Action            string  `json:"action"`
	HubID             string  `json:"hubId,omitempty"`
	CircleID          string  `json:"circleId"`
	Slug              string  `json:"slug"`
	Version           int64   `json:"version,omitempty"`
	CollectionVersion int64   `json:"collectionVersion"`
	ContentHash       string  `json:"contentHash,omitempty"`
	ExpiresAt         *string `json:"expiresAt,omitempty"`
	UpdatedBy         string  `json:"updatedBy,omitempty"`
	UpdatedAt         string  `json:"updatedAt"`
}

func CircleSkillContentHash(content string) string {
	return computeContentHash(content)
}

func CircleSkillManifestETag(circleID string, collectionVersion int64) string {
	shortCircle := computeContentHash(circleID)
	if len(shortCircle) > 12 {
		shortCircle = shortCircle[:12]
	}
	return fmt.Sprintf(`W/"cs-%s-%d"`, shortCircle, collectionVersion)
}

func ValidateCircleSkillSlug(slug string) error {
	if !circleSkillSlugPattern.MatchString(slug) {
		return errors.New("skill slug must match ^[a-z0-9][a-z0-9_-]{0,63}$")
	}
	return nil
}

func ValidateCircleSkillTTL(ttl *int64) error {
	if ttl == nil {
		return nil
	}
	if *ttl == 0 {
		return nil
	}
	if *ttl < MinCircleSkillTTLSeconds || *ttl > MaxCircleSkillTTLSeconds {
		return fmt.Errorf("ttlSeconds must be 0 or between %d and %d", MinCircleSkillTTLSeconds, MaxCircleSkillTTLSeconds)
	}
	return nil
}

func ValidateCircleSkillIdempotencyKey(key string) error {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" || len(trimmed) > 128 || strings.ContainsAny(trimmed, "\r\n") {
		return errors.New("idempotencyKey must be a bounded non-empty value")
	}
	return nil
}

func ValidateCircleSkillContent(content string) (string, string, error) {
	if err := validateUTF8AndSize(content, MaxCircleSkillBytes, "skill"); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(content) == "" {
		return "", "", errors.New("skill content must not be empty")
	}
	for _, r := range content {
		if r == 0 || (unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t') {
			return "", "", errors.New("skill contains invalid control characters")
		}
	}
	if skillRealSecretPattern.MatchString(content) {
		return "", "", errors.New("skill contains real credential or private key material")
	}

	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	bodyStart := 0
	fmTitle := ""
	fmSummary := ""

	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		endIdx := -1
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				endIdx = i
				break
			}
		}
		if endIdx == -1 {
			return "", "", errors.New("unclosed YAML frontmatter")
		}
		var err error
		fmTitle, fmSummary, err = parseSafeFrontmatter(lines[1:endIdx])
		if err != nil {
			return "", "", err
		}
		bodyStart = endIdx + 1
	}

	var fenceChar byte
	var fenceLen int
	var outsideBuf strings.Builder
	derivedTitle := ""
	derivedSummary := ""

	for i := bodyStart; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if fenceLen == 0 {
			if ch, n := parseFenceOpener(trimmed); n >= 3 {
				fenceChar, fenceLen = ch, n
				continue
			}
		} else {
			if isFenceCloser(trimmed, fenceChar, fenceLen) {
				fenceChar, fenceLen = 0, 0
			}
			continue
		}

		outsideBuf.WriteString(line)
		outsideBuf.WriteByte('\n')

		if derivedTitle == "" && strings.HasPrefix(trimmed, "# ") {
			derivedTitle = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		} else if derivedSummary == "" && trimmed != "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "```") {
			derivedSummary = strings.TrimLeft(trimmed, "-*> ")
		}
	}

	outsideText := outsideBuf.String()
	if charterHTMLPattern.MatchString(outsideText) || charterEventPattern.MatchString(outsideText) {
		return "", "", errors.New("skill contains raw HTML or event-handler markup")
	}
	for _, match := range charterCredentialPattern.FindAllStringSubmatch(outsideText, -1) {
		val := extractCredentialAssignedValue(match[0])
		if !skillPlaceholderValue.MatchString(val) {
			return "", "", errors.New("skill contains credential-like content")
		}
	}
	if err := validateMarkdownLinks(outsideText); err != nil {
		return "", "", err
	}

	title := fmTitle
	if title == "" {
		title = derivedTitle
	}
	summary := fmSummary
	if summary == "" {
		summary = derivedSummary
	}
	return truncateRunes(title, MaxCircleSkillTitleLength), truncateRunes(summary, MaxCircleSkillSummaryLength), nil
}

func ValidateCircleSkillInput(slug string, input CircleSkillInput) (string, string, error) {
	if err := ValidateCircleSkillSlug(slug); err != nil {
		return "", "", err
	}
	if err := ValidateCircleSkillIdempotencyKey(input.IdempotencyKey); err != nil {
		return "", "", err
	}
	if err := ValidateCircleSkillTTL(input.TTLSeconds); err != nil {
		return "", "", err
	}
	if input.ExpectedVersion < 0 {
		return "", "", errors.New("expectedVersion must be non-negative")
	}
	title, summary, err := ValidateCircleSkillContent(input.Content)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(input.Title) != "" {
		title = truncateRunes(strings.TrimSpace(input.Title), MaxCircleSkillTitleLength)
	}
	if strings.TrimSpace(input.Summary) != "" {
		summary = truncateRunes(strings.TrimSpace(input.Summary), MaxCircleSkillSummaryLength)
	}
	if title == "" {
		title = slug
	}
	return title, summary, nil
}

func ValidateCircleSkillProposalInput(slug string, input CircleSkillProposalInput) (string, string, error) {
	if err := ValidateCircleSkillSlug(slug); err != nil {
		return "", "", err
	}
	if err := ValidateCircleSkillIdempotencyKey(input.IdempotencyKey); err != nil {
		return "", "", err
	}
	if err := ValidateCircleSkillTTL(input.RequestedTTLSeconds); err != nil {
		return "", "", err
	}
	if input.BaseVersion < 0 {
		return "", "", errors.New("baseVersion must be non-negative")
	}
	title, summary, err := ValidateCircleSkillContent(input.Content)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(input.Title) != "" {
		title = truncateRunes(strings.TrimSpace(input.Title), MaxCircleSkillTitleLength)
	}
	if strings.TrimSpace(input.Summary) != "" {
		summary = truncateRunes(strings.TrimSpace(input.Summary), MaxCircleSkillSummaryLength)
	}
	if title == "" {
		title = slug
	}
	return title, summary, nil
}

func parseSafeFrontmatter(fmLines []string) (string, string, error) {
	allowedKeys := map[string]bool{
		"name":        true,
		"title":       true,
		"description": true,
		"version":     true,
		"tags":        true,
	}
	title := ""
	summary := ""
	inTagsList := false

	for _, rawLine := range fmLines {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if skillYAMLTagOrAliasPattern.MatchString(trimmed) {
			return "", "", errors.New("frontmatter contains disallowed YAML tags or aliases")
		}
		if charterHTMLPattern.MatchString(trimmed) || charterEventPattern.MatchString(trimmed) {
			return "", "", errors.New("frontmatter contains raw HTML or event-handler markup")
		}
		if inTagsList && strings.HasPrefix(trimmed, "- ") {
			continue
		}
		inTagsList = false
		colonIdx := strings.IndexByte(trimmed, ':')
		if colonIdx <= 0 {
			return "", "", errors.New("frontmatter lines must be key: value pairs")
		}
		key := strings.ToLower(strings.TrimSpace(trimmed[:colonIdx]))
		if !allowedKeys[key] {
			return "", "", fmt.Errorf("frontmatter key %q is not allowlisted", key)
		}
		val := strings.TrimSpace(trimmed[colonIdx+1:])
		val = strings.Trim(val, `"'`)
		if key == "tags" && val == "" {
			inTagsList = true
			continue
		}
		if key == "name" || key == "title" {
			if title == "" || key == "title" {
				title = val
			}
		} else if key == "description" {
			summary = val
		}
	}
	return title, summary, nil
}

func parseFenceOpener(trimmed string) (byte, int) {
	if len(trimmed) < 3 {
		return 0, 0
	}
	ch := trimmed[0]
	if ch != '`' && ch != '~' {
		return 0, 0
	}
	count := 0
	for count < len(trimmed) && trimmed[count] == ch {
		count++
	}
	if count < 3 {
		return 0, 0
	}
	if ch == '`' && strings.ContainsRune(trimmed[count:], '`') {
		return 0, 0
	}
	return ch, count
}

func isFenceCloser(trimmed string, ch byte, minLen int) bool {
	if len(trimmed) < minLen {
		return false
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != ch {
			return false
		}
	}
	return true
}

func validateMarkdownLinks(text string) error {
	for _, match := range skillInlineTargetPattern.FindAllStringSubmatch(text, -1) {
		if err := validateSkillLinkTarget(match[1]); err != nil {
			return err
		}
	}
	for _, match := range skillRefDefinitionPattern.FindAllStringSubmatch(text, -1) {
		if err := validateSkillLinkTarget(match[1]); err != nil {
			return err
		}
	}
	for _, match := range skillAngleLinkPattern.FindAllStringSubmatch(text, -1) {
		if err := validateSkillLinkTarget(match[1]); err != nil {
			return err
		}
	}
	return nil
}

func validateSkillLinkTarget(target string) error {
	clean := strings.TrimSpace(strings.Trim(target, "<>"))
	if clean == "" {
		return nil
	}
	lower := strings.ToLower(clean)
	if skillUnsafeSchemePattern.MatchString(lower) {
		return fmt.Errorf("skill link uses disallowed URI scheme: %s", clean)
	}
	if strings.Contains(clean, "..") || strings.ContainsRune(clean, '\\') {
		return errors.New("skill link contains path traversal characters")
	}
	if strings.HasPrefix(lower, "http://") {
		return errors.New("skill link must use https:// rather than http://")
	}
	if strings.HasPrefix(clean, "//") {
		return errors.New("skill link must use explicit https:// or #anchor")
	}
	if strings.HasPrefix(lower, "https://") || strings.HasPrefix(clean, "#") {
		return nil
	}
	if strings.ContainsRune(clean, ':') {
		return fmt.Errorf("skill link uses unsupported URI scheme: %s", clean)
	}
	return nil
}

func extractCredentialAssignedValue(snippet string) string {
	idx := strings.IndexAny(snippet, ":=")
	if idx == -1 {
		fields := strings.Fields(snippet)
		if len(fields) >= 2 {
			return strings.TrimSpace(fields[len(fields)-1])
		}
		return ""
	}
	return strings.TrimSpace(snippet[idx+1:])
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:max]))
}
