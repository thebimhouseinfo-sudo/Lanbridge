package permissions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/store"
)

const (
	CapRead    = "read"
	CapSync    = "sync"
	CapExecute = "execute"
	CapMCP     = "mcp"
)

// Gatekeeper manages and enforces folder permissions
type Gatekeeper struct {
	mu        sync.RWMutex
	store     *store.StateStore
	cfg       *store.HostConfig
	grants    []store.PermissionGrant
	pendingMu sync.Mutex
	pending   map[string]*pendingPermission
}

type PermissionRequest struct {
	ID           string   `json:"id"`
	Path         string   `json:"path"`
	Capabilities []string `json:"capabilities"`
}

type permissionDecision struct {
	approved     bool
	capabilities []string
	err          error
}

type pendingPermission struct {
	request   PermissionRequest
	decision  chan permissionDecision
	resolving bool
}

func NewGatekeeper(stateStore *store.StateStore, cfg *store.HostConfig) (*Gatekeeper, error) {
	state, err := stateStore.Load()
	if err != nil {
		return nil, err
	}

	return &Gatekeeper{
		store:   stateStore,
		cfg:     cfg,
		grants:  state.Permissions,
		pending: make(map[string]*pendingPermission),
	}, nil
}

// BeginRequest queues a client permission request for host-user review.
func (g *Gatekeeper) BeginRequest(id, path string, capabilities []string) error {
	if id == "" {
		return errors.New("permission request ID cannot be empty")
	}
	canonical, err := canonicalDirectory(path)
	if err != nil {
		return err
	}
	requested := uniqueCapabilities(capabilities)
	if len(requested) == 0 {
		return errors.New("permission request must include at least one capability")
	}
	for _, capability := range requested {
		if !isKnownCapability(capability) {
			return fmt.Errorf("unknown capability: %s", capability)
		}
	}

	g.pendingMu.Lock()
	defer g.pendingMu.Unlock()
	if _, exists := g.pending[id]; exists {
		return fmt.Errorf("permission request %q is already pending", id)
	}
	g.pending[id] = &pendingPermission{
		request: PermissionRequest{
			ID:           id,
			Path:         canonical,
			Capabilities: requested,
		},
		decision: make(chan permissionDecision, 1),
	}
	return nil
}

// PendingRequests returns a detached snapshot suitable for UI state.
func (g *Gatekeeper) PendingRequests() []PermissionRequest {
	g.pendingMu.Lock()
	defer g.pendingMu.Unlock()
	requests := make([]PermissionRequest, 0, len(g.pending))
	for _, pending := range g.pending {
		if pending.resolving {
			continue
		}
		request := pending.request
		request.Capabilities = append([]string(nil), request.Capabilities...)
		requests = append(requests, request)
	}
	return requests
}

// RespondRequest persists the Windows user's ACTUAL grant. The host may grant\n// more or fewer capabilities than the Mac requested.
func (g *Gatekeeper) RespondRequest(id string, approved bool, capabilities []string) error {
	g.pendingMu.Lock()
	pending, exists := g.pending[id]
	if !exists {
		g.pendingMu.Unlock()
		return fmt.Errorf("permission request %q is no longer pending", id)
	}
	if pending.resolving {
		g.pendingMu.Unlock()
		return fmt.Errorf("permission request %q is already being resolved", id)
	}

	selected := uniqueCapabilities(capabilities)
	pending.resolving = true
	if approved {
		if len(selected) == 0 {
			pending.resolving = false
			g.pendingMu.Unlock()
			return errors.New("select at least one capability or reject the request")
		}
		for _, capability := range selected {
			if !isKnownCapability(capability) {
				pending.resolving = false
				g.pendingMu.Unlock()
				return fmt.Errorf("unknown capability: %s", capability)
			}
		}
	}
	g.pendingMu.Unlock()

	if approved {
		if err := g.AddGrant(pending.request.Path, selected); err != nil {
			g.pendingMu.Lock()
			if g.pending[id] == pending {
				delete(g.pending, id)
			}
			g.pendingMu.Unlock()
			pending.decision <- permissionDecision{err: err}
			return err
		}
	}

	pending.decision <- permissionDecision{approved: approved, capabilities: selected}
	return nil
}

// WaitForRequest waits for the host user's decision or the configured timeout.
func (g *Gatekeeper) WaitForRequest(ctx context.Context, id string) ([]string, bool, error) {
	g.pendingMu.Lock()
	pending, exists := g.pending[id]
	g.pendingMu.Unlock()
	if !exists {
		return nil, false, fmt.Errorf("permission request %q is no longer pending", id)
	}

	select {
	case decision := <-pending.decision:
		g.pendingMu.Lock()
		if g.pending[id] == pending {
			delete(g.pending, id)
		}
		g.pendingMu.Unlock()
		return decision.capabilities, decision.approved, decision.err
	case <-ctx.Done():
		g.pendingMu.Lock()
		resolving := g.pending[id] == pending && pending.resolving
		if g.pending[id] == pending && !resolving {
			delete(g.pending, id)
		}
		g.pendingMu.Unlock()
		if resolving {
			decision := <-pending.decision
			g.pendingMu.Lock()
			if g.pending[id] == pending {
				delete(g.pending, id)
			}
			g.pendingMu.Unlock()
			return decision.capabilities, decision.approved, nil
		}
		return nil, false, ctx.Err()
	}
}

// CancelPendingRequests rejects every open request, typically when the peer disconnects.
func (g *Gatekeeper) CancelPendingRequests() {
	g.pendingMu.Lock()
	pending := make([]*pendingPermission, 0, len(g.pending))
	for id, request := range g.pending {
		if request.resolving {
			continue
		}
		delete(g.pending, id)
		pending = append(pending, request)
	}
	g.pendingMu.Unlock()

	for _, request := range pending {
		request.decision <- permissionDecision{}
	}
}

// CheckAccess checks if a target path is allowed for a specific capability
func (g *Gatekeeper) CheckAccess(candidatePath string, capability string) bool {
	_, err := g.ResolveAccess(candidatePath, capability)
	return err == nil
}

// HasPermission reports whether a path is covered by a grant for the capability.
func (g *Gatekeeper) HasPermission(candidatePath string, capability string) bool {
	return g.CheckAccess(candidatePath, capability)
}

// ResolveAccess returns the canonical candidate path when a grant authorizes it.
func (g *Gatekeeper) ResolveAccess(candidatePath string, capability string) (string, error) {
	if !filepath.IsAbs(candidatePath) {
		return "", fmt.Errorf("%w: candidate path must be absolute", ErrPathEscape)
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	candidate, err := filepath.Abs(filepath.Clean(candidatePath))
	if err != nil {
		return "", fmt.Errorf("resolve candidate path: %w", err)
	}
	candidate, err = resolveExistingAncestor(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve candidate path: %w", err)
	}
	for _, grant := range g.grants {
		if !hasCapability(grant.Capabilities, capability) {
			continue
		}
		base, err := filepath.Abs(filepath.Clean(grant.Path))
		if err != nil || !isSubPath(base, candidate) {
			continue
		}
		rel, err := filepath.Rel(base, candidate)
		if err != nil {
			continue
		}
		resolved, err := Resolve(base, rel)
		if err == nil {
			return resolved, nil
		}
	}
	return "", ErrAccessDenied
}

// ValidateGrantConstraints checks project constraints from CRITERIA §5
func (g *Gatekeeper) ValidateGrantConstraints(path string, caps []string) error {
	canonical, err := canonicalDirectory(path)
	if err != nil {
		return err
	}
	if g.cfg == nil {
		return errors.New("host config is required to validate permission grants")
	}
	if len(caps) == 0 {
		return errors.New("at least one capability must be granted")
	}

	for _, c := range caps {
		switch c {
		case CapSync:
			// A Windows user's folder grant is authoritative. The project registry
			// does not limit where an explicitly granted Sync can target.
		case CapExecute, CapMCP:
			// Execute and MCP grants are attached to a project root.
			allowed := false
			for _, p := range g.cfg.Projects {
				root, rootErr := canonicalDirectory(p.Root)
				if rootErr == nil && samePath(root, canonical) {
					allowed = true
					break
				}
			}
			if !allowed {
				return errors.New("execute and mcp capabilities can only be granted to project root")
			}
		case CapRead:
			// read is allowed for any directory user approves
		default:
			return fmt.Errorf("unknown capability: %s", c)
		}
	}
	return nil
}

// AddGrant adds an approved grant and persists to state.json
func (g *Gatekeeper) AddGrant(path string, caps []string) error {
	if err := g.ValidateGrantConstraints(path, caps); err != nil {
		return err
	}
	canonical, err := canonicalDirectory(path)
	if err != nil {
		return err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	next := append([]store.PermissionGrant(nil), g.grants...)
	// Update existing grant if path matches
	found := false
	for i, grant := range next {
		if samePath(grant.Path, canonical) {
			next[i].Capabilities = append([]string(nil), caps...)
			next[i].GrantedAt = time.Now().UTC().Format(time.RFC3339)
			found = true
			break
		}
	}

	if !found {
		next = append(next, store.PermissionGrant{
			Path:         canonical,
			Capabilities: append([]string(nil), caps...),
			GrantedAt:    time.Now().UTC().Format(time.RFC3339),
		})
	}

	state, err := g.store.Load()
	if err != nil {
		return err
	}
	state.Permissions = next
	if err := g.store.Save(state); err != nil {
		return err
	}
	g.grants = next
	return nil
}

// RevokeGrant removes permissions for a path
func (g *Gatekeeper) RevokeGrant(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: permission path must be absolute", ErrPathEscape)
	}
	canonical, err := resolveExistingAncestor(path)
	if err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	newGrants := make([]store.PermissionGrant, 0, len(g.grants))
	for _, grant := range g.grants {
		if samePath(grant.Path, canonical) {
			continue
		}
		newGrants = append(newGrants, grant)
	}
	state, err := g.store.Load()
	if err != nil {
		return err
	}
	state.Permissions = newGrants
	if err := g.store.Save(state); err != nil {
		return err
	}
	g.grants = newGrants
	return nil
}

// Unpair clears the stored peer fingerprint and all grants, then cancels pending requests.
func (g *Gatekeeper) Unpair() error {
	g.mu.Lock()
	state, err := g.store.Load()
	if err != nil {
		g.mu.Unlock()
		return err
	}
	state.PairedPeerFingerprint = ""
	state.PendingUnpair = false
	state.Permissions = []store.PermissionGrant{}
	if err := g.store.Save(state); err != nil {
		g.mu.Unlock()
		return err
	}
	g.grants = []store.PermissionGrant{}
	g.mu.Unlock()

	g.CancelPendingRequests()
	return nil
}

// ListGrants returns a copy of current grants
func (g *Gatekeeper) ListGrants() []store.PermissionGrant {
	g.mu.RLock()
	defer g.mu.RUnlock()
	res := make([]store.PermissionGrant, len(g.grants))
	for i, grant := range g.grants {
		res[i] = grant
		res[i].Capabilities = append([]string(nil), grant.Capabilities...)
	}
	return res
}

func hasCapability(capabilities []string, capability string) bool {
	for _, current := range capabilities {
		if current == capability {
			return true
		}
	}
	return false
}

func isKnownCapability(capability string) bool {
	switch capability {
	case CapRead, CapSync, CapExecute, CapMCP:
		return true
	default:
		return false
	}
}

func canonicalDirectory(path string) (string, error) {
	if path == "" {
		return "", errors.New("permission path cannot be empty")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: permission path must be absolute", ErrPathEscape)
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve permission path: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve permission directory %q: %w", path, err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("inspect permission directory %q: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("permission path %q is not a directory", path)
	}
	return filepath.Clean(canonical), nil
}

func isWithinConfiguredDirectory(base, candidate string) bool {
	canonicalBase, err := canonicalDirectory(base)
	return err == nil && isSubPath(canonicalBase, candidate)
}

func samePath(left, right string) bool {
	return isSubPath(left, right) && isSubPath(right, left)
}

func uniqueCapabilities(capabilities []string) []string {
	result := make([]string, 0, len(capabilities))
	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if _, exists := seen[capability]; exists {
			continue
		}
		seen[capability] = struct{}{}
		result = append(result, capability)
	}
	return result
}
