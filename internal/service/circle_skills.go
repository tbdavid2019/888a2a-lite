package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

type circleSkillWriteRequest struct {
	Title           string `json:"title,omitempty"`
	Summary         string `json:"summary,omitempty"`
	Content         string `json:"content"`
	TTLSeconds      *int64 `json:"ttlSeconds,omitempty"`
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

type circleSkillProposalRequest struct {
	Title               string `json:"title,omitempty"`
	Summary             string `json:"summary,omitempty"`
	Content             string `json:"content"`
	BaseVersion         int64  `json:"baseVersion"`
	RequestedTTLSeconds *int64 `json:"requestedTtlSeconds,omitempty"`
	IdempotencyKey      string `json:"idempotencyKey"`
}

type circleSkillDecisionRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	TTLSeconds      *int64 `json:"ttlSeconds,omitempty"`
	Reason          string `json:"reason,omitempty"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

type circleSkillDeleteRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

func (service *Service) canHumanManageCircleSkills(agent hub.RegisteredAgent) bool {
	if service.config.CircleSkillWritePolicy == "admin_only" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(agent.ProviderFamily), "browser")
}

func (service *Service) requireCircleExists(ctx context.Context, circleID string) error {
	if strings.TrimSpace(circleID) == "" {
		return fmt.Errorf("%w: circleId is required", ErrValidation)
	}
	if _, err := service.store.Circles().FindCircle(ctx, circleID); err != nil {
		if errors.Is(err, store.ErrNotFound) && circleID == "public" {
			return nil
		}
		return err
	}
	return nil
}

func (service *Service) publishCircleSkillEvent(event hub.CircleSkillEvent) {
	if service.broker != nil {
		service.broker.PublishCircleSkillEvent(event)
	}
}

func (service *Service) ListCircleSkillManifest(ctx context.Context, agentID, token string) (hub.CircleSkillManifest, error) {
	agent, err := service.authenticateAgentRecord(ctx, agentID, token)
	if err != nil {
		return hub.CircleSkillManifest{}, err
	}
	return service.store.CircleSkills().ListCircleSkillManifest(ctx, service.config.HubID, agent.CircleID, service.now().UTC())
}

func (service *Service) GetCircleSkill(ctx context.Context, agentID, token, slug string) (hub.CircleSkill, error) {
	agent, err := service.authenticateAgentRecord(ctx, agentID, token)
	if err != nil {
		return hub.CircleSkill{}, err
	}
	if err := hub.ValidateCircleSkillSlug(slug); err != nil {
		return hub.CircleSkill{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	return service.store.CircleSkills().GetCircleSkill(ctx, service.config.HubID, agent.CircleID, slug, service.now().UTC())
}

func (service *Service) ListCircleSkillRevisions(ctx context.Context, agentID, token, slug string) ([]hub.CircleSkillRevision, error) {
	agent, err := service.authenticateAgentRecord(ctx, agentID, token)
	if err != nil {
		return nil, err
	}
	if err := hub.ValidateCircleSkillSlug(slug); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	return service.store.CircleSkills().ListCircleSkillRevisions(ctx, service.config.HubID, agent.CircleID, slug)
}

func (service *Service) ListCircleSkillProposals(ctx context.Context, agentID, token string) ([]hub.CircleSkillProposal, error) {
	agent, err := service.authenticateAgentRecord(ctx, agentID, token)
	if err != nil {
		return nil, err
	}
	return service.store.CircleSkills().ListCircleSkillProposals(ctx, service.config.HubID, agent.CircleID)
}

func (service *Service) SubmitCircleSkillProposal(ctx context.Context, agentID, token, slug string, input hub.CircleSkillProposalInput) (hub.CircleSkillProposal, bool, error) {
	agent, err := service.authenticateAgentRecord(ctx, agentID, token)
	if err != nil {
		return hub.CircleSkillProposal{}, false, err
	}
	title, summary, err := hub.ValidateCircleSkillProposalInput(slug, input)
	if err != nil {
		return hub.CircleSkillProposal{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	proposal := hub.CircleSkillProposal{
		HubID:               service.config.HubID,
		CircleID:            agent.CircleID,
		Slug:                slug,
		BaseVersion:         input.BaseVersion,
		Title:               title,
		Summary:             summary,
		Content:             input.Content,
		ContentHash:         hub.CircleSkillContentHash(input.Content),
		RequestedTTLSeconds: input.RequestedTTLSeconds,
		ProposedBy:          agentID,
	}
	saved, colVersion, isUpdate, duplicate, err := service.store.CircleSkills().SubmitCircleSkillProposal(ctx, proposal, strings.TrimSpace(input.IdempotencyKey), service.now().UTC())
	if err != nil {
		return hub.CircleSkillProposal{}, duplicate, err
	}
	if !duplicate {
		action := hub.SkillEventActionProposalCreated
		if isUpdate {
			action = hub.SkillEventActionProposalUpdated
		}
		service.audit(ctx, hub.Event{
			Type:         hub.EventCircleSkillProposed,
			CircleID:     agent.CircleID,
			ActorAgentID: agentID,
			Details:      map[string]any{"slug": slug, "contentHash": saved.ContentHash, "action": action},
		})
		service.publishCircleSkillEvent(hub.CircleSkillEvent{
			Action:            action,
			HubID:             service.config.HubID,
			CircleID:          agent.CircleID,
			Slug:              slug,
			CollectionVersion: colVersion,
			ContentHash:       saved.ContentHash,
			UpdatedBy:         agentID,
			UpdatedAt:         saved.UpdatedAt,
		})
	}
	return saved, duplicate, nil
}

func (service *Service) PutCircleSkillByMember(ctx context.Context, agentID, token, slug string, input hub.CircleSkillInput) (hub.CircleSkill, bool, error) {
	agent, err := service.authenticateAgentRecord(ctx, agentID, token)
	if err != nil {
		return hub.CircleSkill{}, false, err
	}
	if !service.canHumanManageCircleSkills(agent) {
		return hub.CircleSkill{}, false, ErrForbidden
	}
	return service.putCircleSkillInternal(ctx, agent.CircleID, agentID, slug, input)
}

func (service *Service) DeleteCircleSkillByMember(ctx context.Context, agentID, token, slug string, input hub.CircleSkillDeleteInput) (int64, bool, error) {
	agent, err := service.authenticateAgentRecord(ctx, agentID, token)
	if err != nil {
		return 0, false, err
	}
	if !service.canHumanManageCircleSkills(agent) {
		return 0, false, ErrForbidden
	}
	return service.deleteCircleSkillInternal(ctx, agent.CircleID, agentID, slug, input)
}

func (service *Service) ApproveCircleSkillProposalByMember(ctx context.Context, agentID, token, slug string, input hub.CircleSkillDecisionInput) (hub.CircleSkill, bool, error) {
	agent, err := service.authenticateAgentRecord(ctx, agentID, token)
	if err != nil {
		return hub.CircleSkill{}, false, err
	}
	if !service.canHumanManageCircleSkills(agent) {
		return hub.CircleSkill{}, false, ErrForbidden
	}
	return service.approveCircleSkillProposalInternal(ctx, agent.CircleID, agentID, slug, input)
}

func (service *Service) RejectCircleSkillProposalByMember(ctx context.Context, agentID, token, slug string, input hub.CircleSkillDecisionInput) (hub.CircleSkillProposal, bool, error) {
	agent, err := service.authenticateAgentRecord(ctx, agentID, token)
	if err != nil {
		return hub.CircleSkillProposal{}, false, err
	}
	if !service.canHumanManageCircleSkills(agent) {
		return hub.CircleSkillProposal{}, false, ErrForbidden
	}
	return service.rejectCircleSkillProposalInternal(ctx, agent.CircleID, agentID, slug, input)
}

func (service *Service) AdminListCircleSkills(ctx context.Context, opToken, circleID string) (hub.CircleSkillManifest, []hub.CircleSkillProposal, error) {
	if err := service.AuthenticateOperator(opToken); err != nil {
		return hub.CircleSkillManifest{}, nil, err
	}
	if err := service.requireCircleExists(ctx, circleID); err != nil {
		return hub.CircleSkillManifest{}, nil, err
	}
	manifest, err := service.store.CircleSkills().ListCircleSkillManifest(ctx, service.config.HubID, circleID, service.now().UTC())
	if err != nil {
		return hub.CircleSkillManifest{}, nil, err
	}
	proposals, err := service.store.CircleSkills().ListCircleSkillProposals(ctx, service.config.HubID, circleID)
	if err != nil {
		return hub.CircleSkillManifest{}, nil, err
	}
	return manifest, proposals, nil
}

func (service *Service) AdminGetCircleSkill(ctx context.Context, opToken, circleID, slug string) (hub.CircleSkill, error) {
	if err := service.AuthenticateOperator(opToken); err != nil {
		return hub.CircleSkill{}, err
	}
	if err := hub.ValidateCircleSkillSlug(slug); err != nil {
		return hub.CircleSkill{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := service.requireCircleExists(ctx, circleID); err != nil {
		return hub.CircleSkill{}, err
	}
	return service.store.CircleSkills().GetCircleSkill(ctx, service.config.HubID, circleID, slug, service.now().UTC())
}

func (service *Service) AdminListCircleSkillRevisions(ctx context.Context, opToken, circleID, slug string) ([]hub.CircleSkillRevision, error) {
	if err := service.AuthenticateOperator(opToken); err != nil {
		return nil, err
	}
	if err := hub.ValidateCircleSkillSlug(slug); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := service.requireCircleExists(ctx, circleID); err != nil {
		return nil, err
	}
	return service.store.CircleSkills().ListCircleSkillRevisions(ctx, service.config.HubID, circleID, slug)
}

func (service *Service) AdminPutCircleSkill(ctx context.Context, opToken, circleID, slug string, input hub.CircleSkillInput) (hub.CircleSkill, bool, error) {
	if err := service.AuthenticateOperator(opToken); err != nil {
		return hub.CircleSkill{}, false, err
	}
	if err := service.requireCircleExists(ctx, circleID); err != nil {
		return hub.CircleSkill{}, false, err
	}
	return service.putCircleSkillInternal(ctx, circleID, "operator", slug, input)
}

func (service *Service) AdminDeleteCircleSkill(ctx context.Context, opToken, circleID, slug string, input hub.CircleSkillDeleteInput) (int64, bool, error) {
	if err := service.AuthenticateOperator(opToken); err != nil {
		return 0, false, err
	}
	if err := service.requireCircleExists(ctx, circleID); err != nil {
		return 0, false, err
	}
	return service.deleteCircleSkillInternal(ctx, circleID, "operator", slug, input)
}

func (service *Service) AdminApproveCircleSkillProposal(ctx context.Context, opToken, circleID, slug string, input hub.CircleSkillDecisionInput) (hub.CircleSkill, bool, error) {
	if err := service.AuthenticateOperator(opToken); err != nil {
		return hub.CircleSkill{}, false, err
	}
	if err := service.requireCircleExists(ctx, circleID); err != nil {
		return hub.CircleSkill{}, false, err
	}
	return service.approveCircleSkillProposalInternal(ctx, circleID, "operator", slug, input)
}

func (service *Service) AdminRejectCircleSkillProposal(ctx context.Context, opToken, circleID, slug string, input hub.CircleSkillDecisionInput) (hub.CircleSkillProposal, bool, error) {
	if err := service.AuthenticateOperator(opToken); err != nil {
		return hub.CircleSkillProposal{}, false, err
	}
	if err := service.requireCircleExists(ctx, circleID); err != nil {
		return hub.CircleSkillProposal{}, false, err
	}
	return service.rejectCircleSkillProposalInternal(ctx, circleID, "operator", slug, input)
}

func (service *Service) putCircleSkillInternal(ctx context.Context, circleID, actor, slug string, input hub.CircleSkillInput) (hub.CircleSkill, bool, error) {
	title, summary, err := hub.ValidateCircleSkillInput(slug, input)
	if err != nil {
		return hub.CircleSkill{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	skill := hub.CircleSkill{
		HubID:       service.config.HubID,
		CircleID:    circleID,
		Slug:        slug,
		Title:       title,
		Summary:     summary,
		Content:     input.Content,
		ContentHash: hub.CircleSkillContentHash(input.Content),
		TTLSeconds:  input.TTLSeconds,
		ApprovedBy:  actor,
		UpdatedBy:   actor,
	}
	saved, colVersion, duplicate, err := service.store.CircleSkills().PutCircleSkill(ctx, skill, input.ExpectedVersion, strings.TrimSpace(input.IdempotencyKey), service.now().UTC())
	if err != nil {
		return hub.CircleSkill{}, duplicate, err
	}
	if !duplicate {
		service.audit(ctx, hub.Event{
			Type:         hub.EventCircleSkillUpdated,
			CircleID:     circleID,
			ActorAgentID: actor,
			Details:      map[string]any{"slug": slug, "version": saved.Version, "collectionVersion": colVersion, "contentHash": saved.ContentHash},
		})
		service.publishCircleSkillEvent(hub.CircleSkillEvent{
			Action:            hub.SkillEventActionUpdated,
			HubID:             service.config.HubID,
			CircleID:          circleID,
			Slug:              slug,
			Version:           saved.Version,
			CollectionVersion: colVersion,
			ContentHash:       saved.ContentHash,
			ExpiresAt:         saved.ExpiresAt,
			UpdatedBy:         actor,
			UpdatedAt:         saved.UpdatedAt,
		})
	}
	return saved, duplicate, nil
}

func (service *Service) deleteCircleSkillInternal(ctx context.Context, circleID, actor, slug string, input hub.CircleSkillDeleteInput) (int64, bool, error) {
	if err := hub.ValidateCircleSkillSlug(slug); err != nil {
		return 0, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := hub.ValidateCircleSkillIdempotencyKey(input.IdempotencyKey); err != nil {
		return 0, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if input.ExpectedVersion < 0 {
		return 0, false, fmt.Errorf("%w: expectedVersion must be non-negative", ErrValidation)
	}
	version, colVersion, duplicate, err := service.store.CircleSkills().DeleteCircleSkill(ctx, service.config.HubID, circleID, slug, actor, input.ExpectedVersion, strings.TrimSpace(input.IdempotencyKey), service.now().UTC())
	if err != nil {
		return 0, duplicate, err
	}
	if !duplicate {
		nowStr := service.now().UTC().Format(time.RFC3339Nano)
		service.audit(ctx, hub.Event{
			Type:         hub.EventCircleSkillDeleted,
			CircleID:     circleID,
			ActorAgentID: actor,
			Details:      map[string]any{"slug": slug, "version": version, "collectionVersion": colVersion},
		})
		service.publishCircleSkillEvent(hub.CircleSkillEvent{
			Action:            hub.SkillEventActionDeleted,
			HubID:             service.config.HubID,
			CircleID:          circleID,
			Slug:              slug,
			Version:           version,
			CollectionVersion: colVersion,
			UpdatedBy:         actor,
			UpdatedAt:         nowStr,
		})
	}
	return version, duplicate, nil
}

func (service *Service) approveCircleSkillProposalInternal(ctx context.Context, circleID, actor, slug string, input hub.CircleSkillDecisionInput) (hub.CircleSkill, bool, error) {
	if err := hub.ValidateCircleSkillSlug(slug); err != nil {
		return hub.CircleSkill{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := hub.ValidateCircleSkillIdempotencyKey(input.IdempotencyKey); err != nil {
		return hub.CircleSkill{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := hub.ValidateCircleSkillTTL(input.TTLSeconds); err != nil {
		return hub.CircleSkill{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if input.ExpectedVersion < 0 {
		return hub.CircleSkill{}, false, fmt.Errorf("%w: expectedVersion must be non-negative", ErrValidation)
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	saved, colVersion, duplicate, err := service.store.CircleSkills().ApproveCircleSkillProposal(ctx, service.config.HubID, circleID, slug, actor, input, service.now().UTC())
	if err != nil {
		return hub.CircleSkill{}, duplicate, err
	}
	if !duplicate {
		service.audit(ctx, hub.Event{
			Type:         hub.EventCircleSkillApproved,
			CircleID:     circleID,
			ActorAgentID: actor,
			Details:      map[string]any{"slug": slug, "version": saved.Version, "collectionVersion": colVersion, "contentHash": saved.ContentHash, "proposedBy": saved.ProposedBy},
		})
		service.publishCircleSkillEvent(hub.CircleSkillEvent{
			Action:            hub.SkillEventActionUpdated,
			HubID:             service.config.HubID,
			CircleID:          circleID,
			Slug:              slug,
			Version:           saved.Version,
			CollectionVersion: colVersion,
			ContentHash:       saved.ContentHash,
			ExpiresAt:         saved.ExpiresAt,
			UpdatedBy:         actor,
			UpdatedAt:         saved.UpdatedAt,
		})
	}
	return saved, duplicate, nil
}

func (service *Service) rejectCircleSkillProposalInternal(ctx context.Context, circleID, actor, slug string, input hub.CircleSkillDecisionInput) (hub.CircleSkillProposal, bool, error) {
	if err := hub.ValidateCircleSkillSlug(slug); err != nil {
		return hub.CircleSkillProposal{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := hub.ValidateCircleSkillIdempotencyKey(input.IdempotencyKey); err != nil {
		return hub.CircleSkillProposal{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if len(input.Reason) > 280 {
		return hub.CircleSkillProposal{}, false, fmt.Errorf("%w: reason exceeds 280 characters", ErrValidation)
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Reason = strings.TrimSpace(input.Reason)
	rejected, colVersion, duplicate, err := service.store.CircleSkills().RejectCircleSkillProposal(ctx, service.config.HubID, circleID, slug, actor, input, service.now().UTC())
	if err != nil {
		return hub.CircleSkillProposal{}, duplicate, err
	}
	if !duplicate {
		service.audit(ctx, hub.Event{
			Type:         hub.EventCircleSkillRejected,
			CircleID:     circleID,
			ActorAgentID: actor,
			Details:      map[string]any{"slug": slug, "contentHash": rejected.ContentHash, "proposedBy": rejected.ProposedBy},
		})
		service.publishCircleSkillEvent(hub.CircleSkillEvent{
			Action:            hub.SkillEventActionProposalRejected,
			HubID:             service.config.HubID,
			CircleID:          circleID,
			Slug:              slug,
			CollectionVersion: colVersion,
			ContentHash:       rejected.ContentHash,
			UpdatedBy:         actor,
			UpdatedAt:         rejected.UpdatedAt,
		})
	}
	return rejected, duplicate, nil
}

// --- HTTP Handlers ---

func etagMatches(headerValue, etag string) bool {
	if headerValue == "" || etag == "" {
		return false
	}
	trimmed := strings.TrimSpace(headerValue)
	if trimmed == "*" || trimmed == etag {
		return true
	}
	for _, part := range strings.Split(trimmed, ",") {
		if strings.TrimSpace(part) == etag {
			return true
		}
	}
	return false
}

func (server *HTTPServer) listCircleSkills(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	manifest, err := server.service.ListCircleSkillManifest(r.Context(), agentID, token)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if manifest.ETag != "" {
		w.Header().Set("ETag", manifest.ETag)
		if etagMatches(r.Header.Get("If-None-Match"), manifest.ETag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	writeJSON(w, http.StatusOK, manifest)
}

func (server *HTTPServer) getCircleSkill(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	skill, err := server.service.GetCircleSkill(r.Context(), agentID, token, r.PathValue("slug"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if skill.ContentHash != "" {
		etag := fmt.Sprintf(`"%s"`, skill.ContentHash)
		w.Header().Set("ETag", etag)
		if etagMatches(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	writeJSON(w, http.StatusOK, skill)
}

func (server *HTTPServer) listCircleSkillRevisions(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	revisions, err := server.service.ListCircleSkillRevisions(r.Context(), agentID, token, r.PathValue("slug"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"slug": r.PathValue("slug"), "revisions": revisions})
}

func (server *HTTPServer) listCircleSkillProposals(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	proposals, err := server.service.ListCircleSkillProposals(r.Context(), agentID, token)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"proposals": proposals})
}

func (server *HTTPServer) submitCircleSkillProposal(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	var req circleSkillProposalRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &req) {
		return
	}
	proposal, duplicate, err := server.service.SubmitCircleSkillProposal(r.Context(), agentID, token, r.PathValue("slug"), hub.CircleSkillProposalInput{
		Title:               req.Title,
		Summary:             req.Summary,
		Content:             req.Content,
		BaseVersion:         req.BaseVersion,
		RequestedTTLSeconds: req.RequestedTTLSeconds,
		IdempotencyKey:      req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	status := http.StatusAccepted
	if duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, proposal)
}

func (server *HTTPServer) putCircleSkill(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	var req circleSkillWriteRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &req) {
		return
	}
	skill, duplicate, err := server.service.PutCircleSkillByMember(r.Context(), agentID, token, r.PathValue("slug"), hub.CircleSkillInput{
		Title:           req.Title,
		Summary:         req.Summary,
		Content:         req.Content,
		TTLSeconds:      req.TTLSeconds,
		ExpectedVersion: req.ExpectedVersion,
		IdempotencyKey:  req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	status := http.StatusCreated
	if duplicate || skill.Version > 1 {
		status = http.StatusOK
	}
	writeJSON(w, status, skill)
}

func parseCircleSkillDeleteRequest(w http.ResponseWriter, r *http.Request, maxBytes int64) (circleSkillDeleteRequest, bool) {
	var req circleSkillDeleteRequest
	if r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0 {
		if !decodeJSON(w, r, maxBytes, &req) {
			return req, false
		}
		return req, true
	}
	if expStr := strings.TrimSpace(r.URL.Query().Get("expectedVersion")); expStr != "" {
		parsed, err := strconv.ParseInt(expStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_QUERY", "expectedVersion must be an integer")
			return req, false
		}
		req.ExpectedVersion = parsed
	}
	req.IdempotencyKey = strings.TrimSpace(r.URL.Query().Get("idempotencyKey"))
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	}
	return req, true
}

func (server *HTTPServer) deleteCircleSkill(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	req, valid := parseCircleSkillDeleteRequest(w, r, server.maxBodyBytes)
	if !valid {
		return
	}
	version, duplicate, err := server.service.DeleteCircleSkillByMember(r.Context(), agentID, token, r.PathValue("slug"), hub.CircleSkillDeleteInput{
		ExpectedVersion: req.ExpectedVersion,
		IdempotencyKey:  req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"slug": r.PathValue("slug"), "version": version, "deleted": true, "duplicate": duplicate})
}

func (server *HTTPServer) approveCircleSkillProposal(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	var req circleSkillDecisionRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &req) {
		return
	}
	skill, duplicate, err := server.service.ApproveCircleSkillProposalByMember(r.Context(), agentID, token, r.PathValue("slug"), hub.CircleSkillDecisionInput{
		ExpectedVersion: req.ExpectedVersion,
		TTLSeconds:      req.TTLSeconds,
		Reason:          req.Reason,
		IdempotencyKey:  req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skill": skill, "duplicate": duplicate})
}

func (server *HTTPServer) rejectCircleSkillProposal(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	var req circleSkillDecisionRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &req) {
		return
	}
	proposal, duplicate, err := server.service.RejectCircleSkillProposalByMember(r.Context(), agentID, token, r.PathValue("slug"), hub.CircleSkillDecisionInput{
		ExpectedVersion: req.ExpectedVersion,
		Reason:          req.Reason,
		IdempotencyKey:  req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposal": proposal, "duplicate": duplicate})
}

// --- Operator Admin Handlers ---

func (server *HTTPServer) adminListCircleSkills(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	manifest, proposals, err := server.service.AdminListCircleSkills(r.Context(), token, r.PathValue("circleId"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"manifest": manifest, "proposals": proposals})
}

func (server *HTTPServer) adminGetCircleSkill(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	skill, err := server.service.AdminGetCircleSkill(r.Context(), token, r.PathValue("circleId"), r.PathValue("slug"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, skill)
}

func (server *HTTPServer) adminListCircleSkillRevisions(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	revisions, err := server.service.AdminListCircleSkillRevisions(r.Context(), token, r.PathValue("circleId"), r.PathValue("slug"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"circleId": r.PathValue("circleId"), "slug": r.PathValue("slug"), "revisions": revisions})
}

func (server *HTTPServer) adminPutCircleSkill(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	var req circleSkillWriteRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &req) {
		return
	}
	skill, duplicate, err := server.service.AdminPutCircleSkill(r.Context(), token, r.PathValue("circleId"), r.PathValue("slug"), hub.CircleSkillInput{
		Title:           req.Title,
		Summary:         req.Summary,
		Content:         req.Content,
		TTLSeconds:      req.TTLSeconds,
		ExpectedVersion: req.ExpectedVersion,
		IdempotencyKey:  req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	status := http.StatusCreated
	if duplicate || skill.Version > 1 {
		status = http.StatusOK
	}
	writeJSON(w, status, skill)
}

func (server *HTTPServer) adminDeleteCircleSkill(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	req, valid := parseCircleSkillDeleteRequest(w, r, server.maxBodyBytes)
	if !valid {
		return
	}
	version, duplicate, err := server.service.AdminDeleteCircleSkill(r.Context(), token, r.PathValue("circleId"), r.PathValue("slug"), hub.CircleSkillDeleteInput{
		ExpectedVersion: req.ExpectedVersion,
		IdempotencyKey:  req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"circleId": r.PathValue("circleId"), "slug": r.PathValue("slug"), "version": version, "deleted": true, "duplicate": duplicate})
}

func (server *HTTPServer) adminApproveCircleSkillProposal(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	var req circleSkillDecisionRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &req) {
		return
	}
	skill, duplicate, err := server.service.AdminApproveCircleSkillProposal(r.Context(), token, r.PathValue("circleId"), r.PathValue("slug"), hub.CircleSkillDecisionInput{
		ExpectedVersion: req.ExpectedVersion,
		TTLSeconds:      req.TTLSeconds,
		Reason:          req.Reason,
		IdempotencyKey:  req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skill": skill, "duplicate": duplicate})
}

func (server *HTTPServer) adminRejectCircleSkillProposal(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	var req circleSkillDecisionRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &req) {
		return
	}
	proposal, duplicate, err := server.service.AdminRejectCircleSkillProposal(r.Context(), token, r.PathValue("circleId"), r.PathValue("slug"), hub.CircleSkillDecisionInput{
		ExpectedVersion: req.ExpectedVersion,
		Reason:          req.Reason,
		IdempotencyKey:  req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposal": proposal, "duplicate": duplicate})
}
