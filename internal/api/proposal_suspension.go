package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/common"
	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
)

// When a namespace has schema proposals turned on, Cosmo refuses to publish a subgraph schema or
// delete a subgraph unless an approved proposal contains exactly that change
// (ERR_SCHEMA_MISMATCH_WITH_APPROVED_PROPOSAL). Terraform has no way to create proposals, so the
// provider turns proposals off for the namespace while it publishes or deletes, then turns them
// back on with the severity levels they had before. Cosmo only blocks the change when the publish
// severity is "error"; with "warn" it publishes or deletes and reports a warning, so proposals are
// left alone then.
//
// Turning proposals off makes Cosmo delete all of the namespace's proposal settings, and turning
// them on again starts from defaults. The provider can only put back the settings it knows about,
// so it doesn't turn proposals off if Cosmo reports settings it doesn't know about (for example ones
// added to Cosmo after this provider was built). After turning proposals back on, it reads the
// settings again and reports an error if they differ from before.
//
// Terraform runs several resources at once, so subgraphs in the same namespace can be published at
// the same time. The provider counts the operations running in each namespace: the first one
// turns proposals off and the last one turns them back on. That way one operation can't turn
// proposals back on while another one in the same namespace is still running.

var (
	// ErrProposalsNotRestored means the change was made, but the provider could not turn the
	// namespace's schema proposals back on afterwards, or could not confirm their settings are as
	// they were before.
	ErrProposalsNotRestored = errors.New("ErrProposalsNotRestored")

	// ErrUnknownProposalSettings means the namespace's schema proposals would block the change, but
	// Cosmo reports proposal settings this provider can't put back, so nothing was changed.
	ErrUnknownProposalSettings = errors.New("ErrUnknownProposalSettings")
)

// proposalRestoreTimeout limits turning proposals back on. It is used even when Terraform is being
// interrupted, because proposals left off are worse than a slightly slower stop. Cosmo rejects
// requests whose timeout is 2 minutes or more, so keep it below that.
const proposalRestoreTimeout = 110 * time.Second

// proposalSuspensions holds one entry per namespace this provider has published to or deleted
// from. Its zero value is ready to use.
type proposalSuspensions struct {
	mu         sync.Mutex
	namespaces map[string]*suspendedNamespace
}

type suspendedNamespace struct {
	// mu is held while proposals are being turned off or back on, so other operations in the
	// namespace wait until that has finished.
	mu sync.Mutex
	// running counts the publishes and deletes in progress in the namespace.
	running int
	// saved is the proposal setting from before the provider turned proposals off. It is nil
	// when there is nothing to turn back on.
	saved *NamespaceProposalConfig
}

func (s *proposalSuspensions) forNamespace(namespace string) *suspendedNamespace {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.namespaces == nil {
		s.namespaces = map[string]*suspendedNamespace{}
	}
	ns, ok := s.namespaces[namespace]
	if !ok {
		ns = &suspendedNamespace{}
		s.namespaces[namespace] = ns
	}
	return ns
}

// withProposalsSuspended runs op with the namespace's schema proposals turned off, if they would
// block it, and turns them back on afterwards with their previous severity levels.
//
// If Cosmo can't report the proposal setting (for example the organization's plan has no
// proposals, the namespace doesn't exist, or the Cosmo server is too old to know about proposals),
// op runs anyway, so its own result is what the caller sees.
func (p *PlatformClient) withProposalsSuspended(ctx context.Context, namespace string, op func() *ApiError) *ApiError {
	ns := p.proposals.forNamespace(namespace)

	readErr, apiErr := p.suspendProposals(ctx, namespace, ns)
	if apiErr != nil {
		return apiErr
	}

	opErr := op()
	restoreErr := p.resumeProposals(ctx, namespace, ns)

	if opErr != nil {
		if readErr != nil && opErr.Status == common.EnumStatusCode_ERR_SCHEMA_MISMATCH_WITH_APPROVED_PROPOSAL {
			opErr.Reason += fmt.Sprintf(" (the provider could not check whether schema proposals are on for namespace %q, "+
				"so it did not turn them off: %s)", namespace, readErr.Error())
		}
		if restoreErr != nil {
			opErr.Reason += "; also, " + restoreErr.Reason
		}
		return opErr
	}

	return restoreErr
}

// suspendProposals turns the namespace's proposals off if this is the first operation running in
// it and proposals would block the operation. readErr is set when the proposal setting couldn't be
// read, in which case nothing was changed. apiErr is set when proposals would block the operation
// but weren't turned off; the operation must not run then.
func (p *PlatformClient) suspendProposals(ctx context.Context, namespace string, ns *suspendedNamespace) (readErr, apiErr *ApiError) {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	if ns.running == 0 {
		config, err := p.GetNamespaceProposalConfig(ctx, namespace)
		switch {
		case err != nil:
			readErr = err
		case !config.Enabled || config.PublishSeverityLevel == platformv1.LintSeverity_warn:
			// Nothing to do: Cosmo won't block the operation.
		case config.HasUnknownSettings():
			return nil, &ApiError{
				Err:    ErrUnknownProposalSettings,
				Status: common.EnumStatusCode_ERR,
				Reason: fmt.Sprintf("Schema proposals are on for namespace %q, and Cosmo only accepts this change if an "+
					"approved proposal contains it. The provider normally turns proposals off while it makes the change "+
					"and back on afterwards, but Cosmo has proposal settings this version of the provider doesn't know "+
					"about, and turning proposals off would lose them. Nothing was changed. Upgrade the provider, or turn "+
					"proposals off for the namespace yourself, apply, and turn them back on.", namespace),
			}
		default:
			if err := p.EnableProposalsForNamespace(ctx, namespace, false); err != nil {
				return nil, &ApiError{
					Err:    err.Err,
					Status: err.Status,
					Reason: fmt.Sprintf("Schema proposals are on for namespace %q, and Cosmo only accepts this change if "+
						"an approved proposal contains it. The provider tried to turn proposals off while it makes the "+
						"change, but could not (the API key needs write access to the namespace): %s", namespace, err.Reason),
				}
			}
			ns.saved = config
		}
	}

	ns.running++
	return readErr, nil
}

// resumeProposals turns the namespace's proposals back on, with their previous severity levels,
// once the last running operation in the namespace has finished. It then reads the settings again
// to check they match what they were before.
func (p *PlatformClient) resumeProposals(ctx context.Context, namespace string, ns *suspendedNamespace) *ApiError {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	ns.running--
	if ns.running > 0 || ns.saved == nil {
		return nil
	}

	saved := ns.saved
	ns.saved = nil

	restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), proposalRestoreTimeout)
	defer cancel()

	// Turning proposals on sets both severity levels to "error", so only set them if they differ.
	err := p.EnableProposalsForNamespace(restoreCtx, namespace, true)
	if err == nil && (saved.CheckSeverityLevel != platformv1.LintSeverity_error || saved.PublishSeverityLevel != platformv1.LintSeverity_error) {
		err = p.ConfigureNamespaceProposalConfig(restoreCtx, namespace, saved.CheckSeverityLevel, saved.PublishSeverityLevel)
	}
	if err != nil {
		return proposalsNotRestored(namespace, saved, err.Status,
			"were turned off for this change and could not be turned back on: "+err.Error())
	}

	after, err := p.GetNamespaceProposalConfig(restoreCtx, namespace)
	if err != nil {
		return proposalsNotRestored(namespace, saved, err.Status,
			"were turned off for this change and turned back on, but the provider could not check that their "+
				"settings are as before: "+err.Error())
	}
	if differences := proposalConfigDifferences(saved, after); len(differences) > 0 {
		return proposalsNotRestored(namespace, saved, common.EnumStatusCode_ERR,
			"were turned off for this change and turned back on, but their settings differ from before: "+
				strings.Join(differences, "; "))
	}

	return nil
}

// proposalConfigDifferences describes how the proposal settings after turning proposals back on
// differ from the settings before.
func proposalConfigDifferences(before, after *NamespaceProposalConfig) []string {
	var differences []string
	if !after.Enabled {
		differences = append(differences, "proposals are off")
	}
	if after.CheckSeverityLevel != before.CheckSeverityLevel {
		differences = append(differences, fmt.Sprintf("check severity is %q instead of %q",
			after.CheckSeverityLevel.String(), before.CheckSeverityLevel.String()))
	}
	if after.PublishSeverityLevel != before.PublishSeverityLevel {
		differences = append(differences, fmt.Sprintf("publish severity is %q instead of %q",
			after.PublishSeverityLevel.String(), before.PublishSeverityLevel.String()))
	}
	if !bytes.Equal(after.unknownSettings, before.unknownSettings) {
		differences = append(differences, "Cosmo reports proposal settings this version of the provider doesn't "+
			"know about, which may have been reset to their defaults")
	}
	return differences
}

func proposalsNotRestored(namespace string, saved *NamespaceProposalConfig, status common.EnumStatusCode, what string) *ApiError {
	return &ApiError{
		Err:    ErrProposalsNotRestored,
		Status: status,
		Reason: fmt.Sprintf("schema proposals for namespace %q %s. Check them in Cosmo Studio: they should be on, "+
			"with check severity %q and publish severity %q (if the namespace is managed by a cosmo_namespace resource "+
			"with proposals set, the next apply sets the severity levels).",
			namespace, what, saved.CheckSeverityLevel.String(), saved.PublishSeverityLevel.String()),
	}
}
