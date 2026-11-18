package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
)

type circleResourceWriteRequest struct {
	Kind            string `json:"kind"`
	Title           string `json:"title,omitempty"`
	Summary         string `json:"summary,omitempty"`
	Content         string `json:"content,omitempty"`
	URL             string `json:"url,omitempty"`
	MediaType       string `json:"mediaType,omitempty"`
	Filename        string `json:"filename,omitempty"`
	SizeBytes       int64  `json:"sizeBytes,omitempty"`
	ContentHash     string `json:"contentHash,omitempty"`
	TTLSeconds      *int64 `json:"ttlSeconds,omitempty"`
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

type circleResourceProposalRequest struct {
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

type circleResourceDecisionRequest struct {
	ExpectedVersion  int64  `json:"expectedVersion"`
	ExpectedRevision int64  `json:"expectedRevision,omitempty"`
	TTLSeconds       *int64 `json:"ttlSeconds,omitempty"`
	Reason           string `json:"reason,omitempty"`
	IdempotencyKey   string `json:"idempotencyKey"`
}

func (service *Service) publishCircleResourceEvent(event hub.CircleResourceEvent) {
	if service.broker != nil {
		service.broker.PublishCircleResourceEvent(event)
	}
}

func (service *Service) ListCircleResourceManifest(ctx context.Context, agentID, token string) (hub.CircleResourceManifest, error) {
	agent, err := service.authenticateAgentRecord(ctx, agentID, token)
	if err != nil {
		return hub.CircleResourceManifest{}, err
	}
	return service.store.CircleResources().ListCircleResourceManifest(ctx, service.config.HubID, agent.CircleID, service.now().UTC())
}

func (service *Service) GetCircleResource(ctx context.Context, agentID, token, slug string) (hub.CircleResource, error) {
	agent, err := service.authenticateAgentRecord(ctx, agentID, token)
	if err != nil {
		return hub.CircleResource{}, err
	}
	if err := hub.ValidateCircleSkillSlug(slug); err != nil {
		return hub.CircleResource{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	return service.store.CircleResources().GetCircleResource(ctx, service.config.HubID, agent.CircleID, slug, service.now().UTC())
}

func (service *Service) ListCircleResourceProposals(ctx context.Context, agentID, token string) ([]hub.CircleResourceProposal, error) {
	agent, err := service.authenticateAgentRecord(ctx, agentID, token)
	if err != nil {
		return nil, err
	}
	return service.store.CircleResources().ListCircleResourceProposals(ctx, service.config.HubID, agent.CircleID)
}

func (service *Service) SubmitCircleResourceProposal(ctx context.Context, agentID, token, slug string, input hub.CircleResourceProposalInput) (hub.CircleResourceProposal, bool, error) {
	agent, err := service.authenticateAgentRecord(ctx, agentID, token)
	if err != nil {
		return hub.CircleResourceProposal{}, false, err
	}
	validated, err := hub.ValidateCircleResourceProposalInput(slug, input)
	if err != nil {
		return hub.CircleResourceProposal{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	proposal := hub.CircleResourceProposal{
		HubID:               service.config.HubID,
		CircleID:            agent.CircleID,
		Slug:                slug,
		Kind:                validated.Kind,
		BaseVersion:         input.BaseVersion,
		Title:               validated.Title,
		Summary:             validated.Summary,
		Content:             validated.Content,
		URL:                 validated.URL,
		MediaType:           validated.MediaType,
		Filename:            validated.Filename,
		SizeBytes:           validated.SizeBytes,
		ContentHash:         validated.ContentHash,
		ByteSize:            validated.ByteSize,
		RequestedTTLSeconds: input.RequestedTTLSeconds,
		ProposedBy:          agentID,
	}
	saved, colVersion, isUpdate, duplicate, err := service.store.CircleResources().SubmitCircleResourceProposal(ctx, proposal, strings.TrimSpace(input.IdempotencyKey), service.now().UTC())
	if err != nil {
		return hub.CircleResourceProposal{}, duplicate, err
	}
	if !duplicate {
		action := hub.ResourceEventActionProposalCreated
		if isUpdate {
			action = hub.ResourceEventActionProposalUpdated
		}
		service.audit(ctx, hub.Event{
			Type:         hub.EventCircleResourceProposed,
			CircleID:     agent.CircleID,
			ActorAgentID: agentID,
			Details:      map[string]any{"slug": slug, "kind": saved.Kind, "contentHash": saved.ContentHash, "action": action},
		})
		service.publishCircleResourceEvent(hub.CircleResourceEvent{
			Action:            action,
			HubID:             service.config.HubID,
			CircleID:          agent.CircleID,
			Slug:              slug,
			Kind:              saved.Kind,
			CollectionVersion: colVersion,
			ContentHash:       saved.ContentHash,
			UpdatedBy:         agentID,
			UpdatedAt:         saved.UpdatedAt,
		})
	}
	return saved, duplicate, nil
}

func (service *Service) PutCircleResourceByMember(ctx context.Context, agentID, token, slug string, input hub.CircleResourceInput) (hub.CircleResource, bool, error) {
	agent, err := service.authenticateAgentRecord(ctx, agentID, token)
	if err != nil {
		return hub.CircleResource{}, false, err
	}
	if !service.canHumanManageCircleSkills(agent) {
		return hub.CircleResource{}, false, ErrForbidden
	}
	return service.putCircleResourceInternal(ctx, agent.CircleID, agentID, slug, input)
}

func (service *Service) DeleteCircleResourceByMember(ctx context.Context, agentID, token, slug string, input hub.CircleResourceDeleteInput) (int64, bool, error) {
	agent, err := service.authenticateAgentRecord(ctx, agentID, token)
	if err != nil {
		return 0, false, err
	}
	if !service.canHumanManageCircleSkills(agent) {
		return 0, false, ErrForbidden
	}
	return service.deleteCircleResourceInternal(ctx, agent.CircleID, agentID, slug, input)
}

func (service *Service) ApproveCircleResourceProposalByMember(ctx context.Context, agentID, token, slug string, input hub.CircleResourceDecisionInput) (hub.CircleResource, bool, error) {
	agent, err := service.authenticateAgentRecord(ctx, agentID, token)
	if err != nil {
		return hub.CircleResource{}, false, err
	}
	if !service.canHumanManageCircleSkills(agent) {
		return hub.CircleResource{}, false, ErrForbidden
	}
	return service.approveCircleResourceProposalInternal(ctx, agent.CircleID, agentID, slug, input)
}

func (service *Service) RejectCircleResourceProposalByMember(ctx context.Context, agentID, token, slug string, input hub.CircleResourceDecisionInput) (hub.CircleResourceProposal, bool, error) {
	agent, err := service.authenticateAgentRecord(ctx, agentID, token)
	if err != nil {
		return hub.CircleResourceProposal{}, false, err
	}
	if !service.canHumanManageCircleSkills(agent) {
		return hub.CircleResourceProposal{}, false, ErrForbidden
	}
	return service.rejectCircleResourceProposalInternal(ctx, agent.CircleID, agentID, slug, input)
}

func (service *Service) AdminListCircleResources(ctx context.Context, opToken, circleID string) (hub.CircleResourceManifest, []hub.CircleResourceProposal, error) {
	if err := service.AuthenticateOperator(opToken); err != nil {
		return hub.CircleResourceManifest{}, nil, err
	}
	if err := service.requireCircleExists(ctx, circleID); err != nil {
		return hub.CircleResourceManifest{}, nil, err
	}
	manifest, err := service.store.CircleResources().ListCircleResourceManifest(ctx, service.config.HubID, circleID, service.now().UTC())
	if err != nil {
		return hub.CircleResourceManifest{}, nil, err
	}
	proposals, err := service.store.CircleResources().ListCircleResourceProposals(ctx, service.config.HubID, circleID)
	if err != nil {
		return hub.CircleResourceManifest{}, nil, err
	}
	return manifest, proposals, nil
}

func (service *Service) AdminGetCircleResource(ctx context.Context, opToken, circleID, slug string) (hub.CircleResource, error) {
	if err := service.AuthenticateOperator(opToken); err != nil {
		return hub.CircleResource{}, err
	}
	if err := hub.ValidateCircleSkillSlug(slug); err != nil {
		return hub.CircleResource{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := service.requireCircleExists(ctx, circleID); err != nil {
		return hub.CircleResource{}, err
	}
	return service.store.CircleResources().GetCircleResource(ctx, service.config.HubID, circleID, slug, service.now().UTC())
}

func (service *Service) AdminPutCircleResource(ctx context.Context, opToken, circleID, slug string, input hub.CircleResourceInput) (hub.CircleResource, bool, error) {
	if err := service.AuthenticateOperator(opToken); err != nil {
		return hub.CircleResource{}, false, err
	}
	if err := service.requireCircleExists(ctx, circleID); err != nil {
		return hub.CircleResource{}, false, err
	}
	return service.putCircleResourceInternal(ctx, circleID, "operator", slug, input)
}

func (service *Service) AdminDeleteCircleResource(ctx context.Context, opToken, circleID, slug string, input hub.CircleResourceDeleteInput) (int64, bool, error) {
	if err := service.AuthenticateOperator(opToken); err != nil {
		return 0, false, err
	}
	if err := service.requireCircleExists(ctx, circleID); err != nil {
		return 0, false, err
	}
	return service.deleteCircleResourceInternal(ctx, circleID, "operator", slug, input)
}

func (service *Service) AdminApproveCircleResourceProposal(ctx context.Context, opToken, circleID, slug string, input hub.CircleResourceDecisionInput) (hub.CircleResource, bool, error) {
	if err := service.AuthenticateOperator(opToken); err != nil {
		return hub.CircleResource{}, false, err
	}
	if err := service.requireCircleExists(ctx, circleID); err != nil {
		return hub.CircleResource{}, false, err
	}
	return service.approveCircleResourceProposalInternal(ctx, circleID, "operator", slug, input)
}

func (service *Service) AdminRejectCircleResourceProposal(ctx context.Context, opToken, circleID, slug string, input hub.CircleResourceDecisionInput) (hub.CircleResourceProposal, bool, error) {
	if err := service.AuthenticateOperator(opToken); err != nil {
		return hub.CircleResourceProposal{}, false, err
	}
	if err := service.requireCircleExists(ctx, circleID); err != nil {
		return hub.CircleResourceProposal{}, false, err
	}
	return service.rejectCircleResourceProposalInternal(ctx, circleID, "operator", slug, input)
}

func (service *Service) putCircleResourceInternal(ctx context.Context, circleID, actor, slug string, input hub.CircleResourceInput) (hub.CircleResource, bool, error) {
	validated, err := hub.ValidateCircleResourceInput(slug, input)
	if err != nil {
		return hub.CircleResource{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	resource := hub.CircleResource{
		HubID:       service.config.HubID,
		CircleID:    circleID,
		Slug:        slug,
		Kind:        validated.Kind,
		Title:       validated.Title,
		Summary:     validated.Summary,
		Content:     validated.Content,
		URL:         validated.URL,
		MediaType:   validated.MediaType,
		Filename:    validated.Filename,
		SizeBytes:   validated.SizeBytes,
		ContentHash: validated.ContentHash,
		ByteSize:    validated.ByteSize,
		TTLSeconds:  input.TTLSeconds,
		ApprovedBy:  actor,
		UpdatedBy:   actor,
	}
	saved, colVersion, duplicate, err := service.store.CircleResources().PutCircleResource(ctx, resource, input.ExpectedVersion, strings.TrimSpace(input.IdempotencyKey), service.now().UTC())
	if err != nil {
		return hub.CircleResource{}, duplicate, err
	}
	if !duplicate {
		service.audit(ctx, hub.Event{
			Type:         hub.EventCircleResourceUpdated,
			CircleID:     circleID,
			ActorAgentID: actor,
			Details:      map[string]any{"slug": slug, "kind": saved.Kind, "version": saved.Version, "collectionVersion": colVersion, "contentHash": saved.ContentHash},
		})
		service.publishCircleResourceEvent(hub.CircleResourceEvent{
			Action:            hub.ResourceEventActionUpdated,
			HubID:             service.config.HubID,
			CircleID:          circleID,
			Slug:              slug,
			Kind:              saved.Kind,
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

func (service *Service) deleteCircleResourceInternal(ctx context.Context, circleID, actor, slug string, input hub.CircleResourceDeleteInput) (int64, bool, error) {
	if err := hub.ValidateCircleSkillSlug(slug); err != nil {
		return 0, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := hub.ValidateCircleSkillIdempotencyKey(input.IdempotencyKey); err != nil {
		return 0, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if input.ExpectedVersion < 0 {
		return 0, false, fmt.Errorf("%w: expectedVersion must be non-negative", ErrValidation)
	}
	version, colVersion, duplicate, err := service.store.CircleResources().DeleteCircleResource(ctx, service.config.HubID, circleID, slug, actor, input.ExpectedVersion, strings.TrimSpace(input.IdempotencyKey), service.now().UTC())
	if err != nil {
		return 0, duplicate, err
	}
	if !duplicate {
		nowStr := service.now().UTC().Format(time.RFC3339Nano)
		service.audit(ctx, hub.Event{
			Type:         hub.EventCircleResourceDeleted,
			CircleID:     circleID,
			ActorAgentID: actor,
			Details:      map[string]any{"slug": slug, "version": version, "collectionVersion": colVersion},
		})
		service.publishCircleResourceEvent(hub.CircleResourceEvent{
			Action:            hub.ResourceEventActionDeleted,
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

func (service *Service) approveCircleResourceProposalInternal(ctx context.Context, circleID, actor, slug string, input hub.CircleResourceDecisionInput) (hub.CircleResource, bool, error) {
	if err := hub.ValidateCircleSkillSlug(slug); err != nil {
		return hub.CircleResource{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := hub.ValidateCircleSkillIdempotencyKey(input.IdempotencyKey); err != nil {
		return hub.CircleResource{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := hub.ValidateCircleSkillTTL(input.TTLSeconds); err != nil {
		return hub.CircleResource{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if input.ExpectedVersion < 0 {
		return hub.CircleResource{}, false, fmt.Errorf("%w: expectedVersion must be non-negative", ErrValidation)
	}
	if input.ExpectedRevision < 0 {
		return hub.CircleResource{}, false, fmt.Errorf("%w: expectedRevision must be non-negative", ErrValidation)
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	saved, colVersion, duplicate, err := service.store.CircleResources().ApproveCircleResourceProposal(ctx, service.config.HubID, circleID, slug, actor, input, service.now().UTC())
	if err != nil {
		return hub.CircleResource{}, duplicate, err
	}
	if !duplicate {
		service.audit(ctx, hub.Event{
			Type:         hub.EventCircleResourceApproved,
			CircleID:     circleID,
			ActorAgentID: actor,
			Details:      map[string]any{"slug": slug, "kind": saved.Kind, "version": saved.Version, "collectionVersion": colVersion, "contentHash": saved.ContentHash, "proposedBy": saved.ProposedBy},
		})
		service.publishCircleResourceEvent(hub.CircleResourceEvent{
			Action:            hub.ResourceEventActionUpdated,
			HubID:             service.config.HubID,
			CircleID:          circleID,
			Slug:              slug,
			Kind:              saved.Kind,
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

func (service *Service) rejectCircleResourceProposalInternal(ctx context.Context, circleID, actor, slug string, input hub.CircleResourceDecisionInput) (hub.CircleResourceProposal, bool, error) {
	if err := hub.ValidateCircleSkillSlug(slug); err != nil {
		return hub.CircleResourceProposal{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if err := hub.ValidateCircleSkillIdempotencyKey(input.IdempotencyKey); err != nil {
		return hub.CircleResourceProposal{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if utf8.RuneCountInString(input.Reason) > 280 {
		return hub.CircleResourceProposal{}, false, fmt.Errorf("%w: reason exceeds 280 characters", ErrValidation)
	}
	if input.ExpectedRevision < 0 {
		return hub.CircleResourceProposal{}, false, fmt.Errorf("%w: expectedRevision must be non-negative", ErrValidation)
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Reason = strings.TrimSpace(input.Reason)
	rejected, colVersion, duplicate, err := service.store.CircleResources().RejectCircleResourceProposal(ctx, service.config.HubID, circleID, slug, actor, input, service.now().UTC())
	if err != nil {
		return hub.CircleResourceProposal{}, duplicate, err
	}
	if !duplicate {
		service.audit(ctx, hub.Event{
			Type:         hub.EventCircleResourceRejected,
			CircleID:     circleID,
			ActorAgentID: actor,
			Details:      map[string]any{"slug": slug, "kind": rejected.Kind, "contentHash": rejected.ContentHash, "proposedBy": rejected.ProposedBy},
		})
		service.publishCircleResourceEvent(hub.CircleResourceEvent{
			Action:            hub.ResourceEventActionProposalRejected,
			HubID:             service.config.HubID,
			CircleID:          circleID,
			Slug:              slug,
			Kind:              rejected.Kind,
			CollectionVersion: colVersion,
			ContentHash:       rejected.ContentHash,
			UpdatedBy:         actor,
			UpdatedAt:         rejected.UpdatedAt,
		})
	}
	return rejected, duplicate, nil
}

// --- HTTP Handlers ---

func (server *HTTPServer) listCircleResources(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	manifest, err := server.service.ListCircleResourceManifest(r.Context(), agentID, token)
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

func (server *HTTPServer) getCircleResource(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	resource, err := server.service.GetCircleResource(r.Context(), agentID, token, r.PathValue("slug"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if etag := hub.CircleResourceItemETag(resource); etag != "" {
		w.Header().Set("ETag", etag)
		if etagMatches(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	writeJSON(w, http.StatusOK, resource)
}

func (server *HTTPServer) listCircleResourceProposals(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	proposals, err := server.service.ListCircleResourceProposals(r.Context(), agentID, token)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"proposals": proposals})
}

func (server *HTTPServer) submitCircleResourceProposal(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	var req circleResourceProposalRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &req) {
		return
	}
	proposal, duplicate, err := server.service.SubmitCircleResourceProposal(r.Context(), agentID, token, r.PathValue("slug"), hub.CircleResourceProposalInput{
		Kind:                req.Kind,
		Title:               req.Title,
		Summary:             req.Summary,
		Content:             req.Content,
		URL:                 req.URL,
		MediaType:           req.MediaType,
		Filename:            req.Filename,
		SizeBytes:           req.SizeBytes,
		ContentHash:         req.ContentHash,
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

func (server *HTTPServer) putCircleResource(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	var req circleResourceWriteRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &req) {
		return
	}
	resource, duplicate, err := server.service.PutCircleResourceByMember(r.Context(), agentID, token, r.PathValue("slug"), hub.CircleResourceInput{
		Kind:            req.Kind,
		Title:           req.Title,
		Summary:         req.Summary,
		Content:         req.Content,
		URL:             req.URL,
		MediaType:       req.MediaType,
		Filename:        req.Filename,
		SizeBytes:       req.SizeBytes,
		ContentHash:     req.ContentHash,
		TTLSeconds:      req.TTLSeconds,
		ExpectedVersion: req.ExpectedVersion,
		IdempotencyKey:  req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	status := http.StatusCreated
	if duplicate || resource.Version > 1 {
		status = http.StatusOK
	}
	writeJSON(w, status, resource)
}

func (server *HTTPServer) deleteCircleResource(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	req, valid := parseCircleSkillDeleteRequest(w, r, server.maxBodyBytes)
	if !valid {
		return
	}
	version, duplicate, err := server.service.DeleteCircleResourceByMember(r.Context(), agentID, token, r.PathValue("slug"), hub.CircleResourceDeleteInput{
		ExpectedVersion: req.ExpectedVersion,
		IdempotencyKey:  req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"slug": r.PathValue("slug"), "version": version, "deleted": true, "duplicate": duplicate})
}

func (server *HTTPServer) approveCircleResourceProposal(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	var req circleResourceDecisionRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &req) {
		return
	}
	resource, duplicate, err := server.service.ApproveCircleResourceProposalByMember(r.Context(), agentID, token, r.PathValue("slug"), hub.CircleResourceDecisionInput{
		ExpectedVersion:  req.ExpectedVersion,
		ExpectedRevision: req.ExpectedRevision,
		TTLSeconds:       req.TTLSeconds,
		Reason:           req.Reason,
		IdempotencyKey:   req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resource": resource, "duplicate": duplicate})
}

func (server *HTTPServer) rejectCircleResourceProposal(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	var req circleResourceDecisionRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &req) {
		return
	}
	proposal, duplicate, err := server.service.RejectCircleResourceProposalByMember(r.Context(), agentID, token, r.PathValue("slug"), hub.CircleResourceDecisionInput{
		ExpectedVersion:  req.ExpectedVersion,
		ExpectedRevision: req.ExpectedRevision,
		Reason:           req.Reason,
		IdempotencyKey:   req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposal": proposal, "duplicate": duplicate})
}

// --- Operator Admin Handlers ---

func (server *HTTPServer) adminListCircleResources(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	manifest, proposals, err := server.service.AdminListCircleResources(r.Context(), token, r.PathValue("circleId"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"manifest": manifest, "proposals": proposals})
}

func (server *HTTPServer) adminGetCircleResource(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	resource, err := server.service.AdminGetCircleResource(r.Context(), token, r.PathValue("circleId"), r.PathValue("slug"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resource)
}

func (server *HTTPServer) adminPutCircleResource(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	var req circleResourceWriteRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &req) {
		return
	}
	resource, duplicate, err := server.service.AdminPutCircleResource(r.Context(), token, r.PathValue("circleId"), r.PathValue("slug"), hub.CircleResourceInput{
		Kind:            req.Kind,
		Title:           req.Title,
		Summary:         req.Summary,
		Content:         req.Content,
		URL:             req.URL,
		MediaType:       req.MediaType,
		Filename:        req.Filename,
		SizeBytes:       req.SizeBytes,
		ContentHash:     req.ContentHash,
		TTLSeconds:      req.TTLSeconds,
		ExpectedVersion: req.ExpectedVersion,
		IdempotencyKey:  req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	status := http.StatusCreated
	if duplicate || resource.Version > 1 {
		status = http.StatusOK
	}
	writeJSON(w, status, resource)
}

func (server *HTTPServer) adminDeleteCircleResource(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	req, valid := parseCircleSkillDeleteRequest(w, r, server.maxBodyBytes)
	if !valid {
		return
	}
	version, duplicate, err := server.service.AdminDeleteCircleResource(r.Context(), token, r.PathValue("circleId"), r.PathValue("slug"), hub.CircleResourceDeleteInput{
		ExpectedVersion: req.ExpectedVersion,
		IdempotencyKey:  req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"circleId": r.PathValue("circleId"), "slug": r.PathValue("slug"), "version": version, "deleted": true, "duplicate": duplicate})
}

func (server *HTTPServer) adminApproveCircleResourceProposal(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	var req circleResourceDecisionRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &req) {
		return
	}
	resource, duplicate, err := server.service.AdminApproveCircleResourceProposal(r.Context(), token, r.PathValue("circleId"), r.PathValue("slug"), hub.CircleResourceDecisionInput{
		ExpectedVersion:  req.ExpectedVersion,
		ExpectedRevision: req.ExpectedRevision,
		TTLSeconds:       req.TTLSeconds,
		Reason:           req.Reason,
		IdempotencyKey:   req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resource": resource, "duplicate": duplicate})
}

func (server *HTTPServer) adminRejectCircleResourceProposal(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	var req circleResourceDecisionRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &req) {
		return
	}
	proposal, duplicate, err := server.service.AdminRejectCircleResourceProposal(r.Context(), token, r.PathValue("circleId"), r.PathValue("slug"), hub.CircleResourceDecisionInput{
		ExpectedVersion:  req.ExpectedVersion,
		ExpectedRevision: req.ExpectedRevision,
		Reason:           req.Reason,
		IdempotencyKey:   req.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposal": proposal, "duplicate": duplicate})
}
