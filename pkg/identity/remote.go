package identity

import (
	"context"
	"net/url"

	"github.com/theflywheel/crest/pkg/client"
)

// PathSubjectBinding is where a subject is exchanged for a party id.
//
// Under /internal/ on purpose, and it is the one place in this package where
// that prefix is load-bearing rather than conventional: the middleware does not
// authenticate /internal/ paths, and it must not, because a service resolving
// the caller's party is exactly the request that cannot itself have a resolved
// caller yet. Requiring a token here would be a bootstrap that never completes.
//
// What that costs is honest: a subject reaching this endpoint gets a party id
// back without a token. It is not a leak of anything a stranger can act on —
// the subject is this deployment's salted pairwise value, so knowing one means
// already having authenticated as that person — but it is a lookup that should
// not be on a public interface. Restricting /internal/ to the service network
// is the deployment's job and is not done by this code.
const PathSubjectBinding = "/internal/identity/subjects/"

// RemoteBinder resolves subjects by asking the registry, for the six services
// that do not own the parties table.
func RemoteBinder(registryBase string) Binder {
	c := client.New(registryBase)
	return BinderFunc(func(ctx context.Context, subject string) (string, error) {
		var out struct {
			PartyID string `json:"partyId"`
		}
		err := c.Get(ctx, PathSubjectBinding+url.PathEscape(subject), &out)
		if err != nil {
			// Not-found is not an error: a subject with no party is somebody
			// authenticated who has not been enrolled here, and enrolment is
			// what fixes that rather than a retry.
			if client.Code(err) == 404 {
				return "", nil
			}
			return "", err
		}
		return out.PartyID, nil
	})
}
