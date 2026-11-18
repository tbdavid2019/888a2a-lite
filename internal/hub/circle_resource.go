package hub

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/tbdavid2019/888a2a-lite/internal/a2a"
)

const (
	CircleResourceExtensionURI        = "https://github.com/tbdavid2019/888a2a-lite/extensions/circle-resources/v1"
	MaxCircleResourcesPerCircle       = 64
	MaxCircleResourceDocBytes         = MaxCircleSkillBytes
	MaxCircleResourceDocsTotalBytes   = 512 * 1024
	MaxCircleResourceProposalsPerCirc = 32
)

const (
	ResourceKindDoc   = "doc"
	ResourceKindAsset = "asset"
)

const (
	ResourceProposalStatusPending  = SkillProposalStatusPending
	ResourceProposalStatusApproved = SkillProposalStatusApproved
	ResourceProposalStatusRejected = SkillProposalStatusRejected
)

const (
	ResourceEventActionUpdated          = "updated"
	ResourceEventActionDeleted          = "deleted"
	ResourceEventActionExpired          = "expired"
	ResourceEventActionProposalCreated  = "proposal_created"
	ResourceEventActionProposalUpdated  = "proposal_updated"
	ResourceEventActionProposalRejected = "proposal_rejected"
)

var sha256HexPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type CircleResource struct {
	HubID       string  `json:"hubId,omitempty"`
	CircleID    string  `json:"circleId,omitempty"`
	Slug        string  `json:"slug"`
	Kind        string  `json:"kind"`
	Title       string  `json:"title"`
	Summary     string  `json:"summary"`
	Content     string  `json:"content,omitempty"`
	URL         string  `json:"url,omitempty"`
	MediaType   string  `json:"mediaType,omitempty"`
	Filename    string  `json:"filename,omitempty"`
	SizeBytes   int64   `json:"sizeBytes,omitempty"`
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
}

type CircleResourceManifestItem struct {
	Slug        string  `json:"slug"`
	Kind        string  `json:"kind"`
	Title       string  `json:"title"`
	Summary     string  `json:"summary"`
	URL         string  `json:"url,omitempty"`
	MediaType   string  `json:"mediaType,omitempty"`
	Filename    string  `json:"filename,omitempty"`
	SizeBytes   int64   `json:"sizeBytes,omitempty"`
	Version     int64   `json:"version"`
	ContentHash string  `json:"contentHash"`
	ByteSize    int     `json:"byteSize"`
	TTLSeconds  *int64  `json:"ttlSeconds,omitempty"`
	ExpiresAt   *string `json:"expiresAt,omitempty"`
	UpdatedBy   string  `json:"updatedBy,omitempty"`
	UpdatedAt   string  `json:"updatedAt"`
}

type CircleResourceManifest struct {
	HubID             string                       `json:"hubId,omitempty"`
	CircleID          string                       `json:"circleId"`
	CollectionVersion int64                        `json:"collectionVersion"`
	ETag              string                       `json:"etag"`
	ResourceCount     int                          `json:"resourceCount"`
	TotalDocBytes     int                          `json:"totalDocBytes"`
	Resources         []CircleResourceManifestItem `json:"resources"`
}

type CircleResourceProposal struct {
	HubID               string `json:"hubId,omitempty"`
	CircleID            string `json:"circleId,omitempty"`
	Slug                string `json:"slug"`
	Kind                string `json:"kind"`
	BaseVersion         int64  `json:"baseVersion"`
	Title               string `json:"title"`
	Summary             string `json:"summary"`
	Content             string `json:"content,omitempty"`
	URL                 string `json:"url,omitempty"`
	MediaType           string `json:"mediaType,omitempty"`
	Filename            string `json:"filename,omitempty"`
	SizeBytes           int64  `json:"sizeBytes,omitempty"`
	ContentHash         string `json:"contentHash"`
	ByteSize            int    `json:"byteSize"`
	RequestedTTLSeconds *int64 `json:"requestedTtlSeconds,omitempty"`
	Status              string `json:"status"`
	ProposedBy          string `json:"proposedBy"`
	ReviewedBy          string `json:"reviewedBy,omitempty"`
	ReviewReason        string `json:"reviewReason,omitempty"`
	CreatedAt           string `json:"createdAt"`
	UpdatedAt           string `json:"updatedAt"`
	ReviewedAt          string `json:"reviewedAt,omitempty"`
}

type CircleResourceInput struct {
	Kind            string `json:"kind"`
	Title           string `json:"title,omitempty"`
	Summary         string `json:"summary,omitempty"`
	Content         string `json:"content,omitempty"`
	URL             string `json:"url,omitempty"`
	MediaType       string `json:"mediaType,omitempty"`
	Filename        string `json:"filename,omitempty"`
	SizeBytes       int64  `json:"sizeBytes,omitempty"`
	ContentHash     string `json:"contentHash,omitempty"`
	ExpectedVersion int64  `json:"expectedVersion"`
	TTLSeconds      *int64 `json:"ttlSeconds,omitempty"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

type CircleResourceProposalInput struct {
	Kind                string `json:"kind"`
	Title               string `json:"title,omitempty"`
	Summary             string `json:"summary,omitempty"`
	Content             string `json:"content,omitempty"`
	URL                 string `json:"url,omitempty"`
	MediaType           string `json:"mediaType,omitempty"`
	Filename            string `json:"filename,omitempty"`
	SizeBytes           int64  `json:"sizeBytes,omitempty"`
	ContentHash         string `json:"contentHash,omitempty"`
	BaseVersion         int64  `json:"baseVersion"`
	RequestedTTLSeconds *int64 `json:"requestedTtlSeconds,omitempty"`
	IdempotencyKey      string `json:"idempotencyKey"`
}

type CircleResourceDecisionInput struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	TTLSeconds      *int64 `json:"ttlSeconds,omitempty"`
	Reason          string `json:"reason,omitempty"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

type CircleResourceDeleteInput struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

type CircleResourceEvent struct {
	Action            string  `json:"action"`
	HubID             string  `json:"hubId,omitempty"`
	CircleID          string  `json:"circleId"`
	Slug              string  `json:"slug"`
	Kind              string  `json:"kind,omitempty"`
	Version           int64   `json:"version,omitempty"`
	CollectionVersion int64   `json:"collectionVersion"`
	ContentHash       string  `json:"contentHash,omitempty"`
	ExpiresAt         *string `json:"expiresAt,omitempty"`
	UpdatedBy         string  `json:"updatedBy,omitempty"`
	UpdatedAt         string  `json:"updatedAt"`
}

type ValidatedCircleResourcePayload struct {
	Kind        string
	Title       string
	Summary     string
	Content     string
	URL         string
	MediaType   string
	Filename    string
	SizeBytes   int64
	ContentHash string
	ByteSize    int
}

func CircleResourceManifestETag(circleID string, collectionVersion int64) string {
	shortCircle := computeContentHash(circleID)
	if len(shortCircle) > 12 {
		shortCircle = shortCircle[:12]
	}
	return fmt.Sprintf(`W/"cr-%s-%d"`, shortCircle, collectionVersion)
}

func ValidateCircleResourcePayload(slug, kind, title, summary, content, rawURL, mediaType, filename string, sizeBytes int64, suppliedHash string) (ValidatedCircleResourcePayload, error) {
	if err := ValidateCircleSkillSlug(slug); err != nil {
		return ValidatedCircleResourcePayload{}, err
	}
	normKind := strings.ToLower(strings.TrimSpace(kind))
	if normKind == "" {
		if strings.TrimSpace(rawURL) != "" {
			normKind = ResourceKindAsset
		} else {
			normKind = ResourceKindDoc
		}
	}
	switch normKind {
	case ResourceKindDoc:
		if strings.TrimSpace(rawURL) != "" {
			return ValidatedCircleResourcePayload{}, errors.New("doc resource must not include url")
		}
		derivedTitle, derivedSummary, err := ValidateCircleSkillContent(content)
		if err != nil {
			return ValidatedCircleResourcePayload{}, err
		}
		finalTitle := truncateRunes(title, MaxCircleSkillTitleLength)
		if finalTitle == "" {
			finalTitle = derivedTitle
		}
		if finalTitle == "" {
			finalTitle = slug
		}
		finalSummary := truncateRunes(summary, MaxCircleSkillSummaryLength)
		if finalSummary == "" {
			finalSummary = derivedSummary
		}
		return ValidatedCircleResourcePayload{
			Kind:        ResourceKindDoc,
			Title:       finalTitle,
			Summary:     finalSummary,
			Content:     content,
			ContentHash: computeContentHash(content),
			ByteSize:    len(content),
		}, nil
	case ResourceKindAsset:
		if strings.TrimSpace(content) != "" {
			return ValidatedCircleResourcePayload{}, errors.New("asset resource must not include inline content")
		}
		cleanURL := strings.TrimSpace(rawURL)
		cleanMedia := strings.TrimSpace(mediaType)
		cleanName := strings.TrimSpace(filename)
		if cleanName == "" && cleanURL != "" {
			if parsed, err := url.Parse(cleanURL); err == nil {
				base := path.Base(parsed.Path)
				if base != "" && base != "." && base != "/" {
					cleanName = base
				}
			}
		}
		part := a2a.Part{
			URL:       &cleanURL,
			MediaType: cleanMedia,
			Filename:  cleanName,
		}
		if err := a2a.ValidatePart(part, a2a.DefaultAttachmentLimits()); err != nil {
			return ValidatedCircleResourcePayload{}, err
		}
		if sizeBytes < 0 || sizeBytes > 10<<30 {
			return ValidatedCircleResourcePayload{}, errors.New("asset sizeBytes is out of valid bounds")
		}
		hash := strings.ToLower(strings.TrimSpace(suppliedHash))
		if hash != "" {
			if !sha256HexPattern.MatchString(hash) {
				return ValidatedCircleResourcePayload{}, errors.New("contentHash must be a 64-character lowercase SHA-256 hex digest")
			}
		} else {
			hash = computeContentHash(fmt.Sprintf("%s\n%s\n%s", cleanURL, cleanMedia, cleanName))
		}
		finalTitle := truncateRunes(title, MaxCircleSkillTitleLength)
		if finalTitle == "" {
			finalTitle = truncateRunes(cleanName, MaxCircleSkillTitleLength)
		}
		if finalTitle == "" {
			finalTitle = slug
		}
		finalSummary := truncateRunes(summary, MaxCircleSkillSummaryLength)
		if finalSummary == "" {
			finalSummary = cleanMedia
		}
		return ValidatedCircleResourcePayload{
			Kind:        ResourceKindAsset,
			Title:       finalTitle,
			Summary:     finalSummary,
			URL:         cleanURL,
			MediaType:   cleanMedia,
			Filename:    cleanName,
			SizeBytes:   sizeBytes,
			ContentHash: hash,
			ByteSize:    0,
		}, nil
	default:
		return ValidatedCircleResourcePayload{}, fmt.Errorf("resource kind must be %q or %q", ResourceKindDoc, ResourceKindAsset)
	}
}

func ValidateCircleResourceInput(slug string, input CircleResourceInput) (ValidatedCircleResourcePayload, error) {
	if err := ValidateCircleSkillIdempotencyKey(input.IdempotencyKey); err != nil {
		return ValidatedCircleResourcePayload{}, err
	}
	if err := ValidateCircleSkillTTL(input.TTLSeconds); err != nil {
		return ValidatedCircleResourcePayload{}, err
	}
	if input.ExpectedVersion < 0 {
		return ValidatedCircleResourcePayload{}, errors.New("expectedVersion must be non-negative")
	}
	return ValidateCircleResourcePayload(
		slug, input.Kind, input.Title, input.Summary,
		input.Content, input.URL, input.MediaType, input.Filename, input.SizeBytes, input.ContentHash,
	)
}

func ValidateCircleResourceProposalInput(slug string, input CircleResourceProposalInput) (ValidatedCircleResourcePayload, error) {
	if err := ValidateCircleSkillIdempotencyKey(input.IdempotencyKey); err != nil {
		return ValidatedCircleResourcePayload{}, err
	}
	if err := ValidateCircleSkillTTL(input.RequestedTTLSeconds); err != nil {
		return ValidatedCircleResourcePayload{}, err
	}
	if input.BaseVersion < 0 {
		return ValidatedCircleResourcePayload{}, errors.New("baseVersion must be non-negative")
	}
	return ValidateCircleResourcePayload(
		slug, input.Kind, input.Title, input.Summary,
		input.Content, input.URL, input.MediaType, input.Filename, input.SizeBytes, input.ContentHash,
	)
}
