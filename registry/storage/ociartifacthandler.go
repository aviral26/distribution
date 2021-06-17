package storage

import (
	"context"
	"fmt"

	"github.com/docker/distribution"
	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/manifest/artifact"
	"github.com/opencontainers/go-digest"
)

// ociArtifactHandler is a ManifestHandler that covers OCI Artifacts.
type ociArtifactHandler struct {
	repository distribution.Repository
	blobStore  distribution.BlobStore
	ctx        context.Context
	referrersStoreFunc
}

func (ah *ociArtifactHandler) Unmarshal(ctx context.Context, dgst digest.Digest, content []byte) (distribution.Manifest, error) {
	dcontext.GetLogger(ah.ctx).Debug("(*artifactHandler).Unmarshal")

	da := &artifact.DeserializedArtifact{}
	if err := da.UnmarshalJSON(content); err != nil {
		return nil, err
	}

	return da, nil
}

func (ah *ociArtifactHandler) Put(ctx context.Context, artifactManfiest distribution.Manifest, skipDependencyVerification bool) (digest.Digest, error) {
	dcontext.GetLogger(ah.ctx).Debug("(*artifactHandler).Put")

	da, ok := artifactManfiest.(*artifact.DeserializedArtifact)
	if !ok {
		return "", fmt.Errorf("wrong type put to artifactHandler: %T", artifactManfiest)
	}

	if err := ah.verifyManifest(ah.ctx, *da, skipDependencyVerification); err != nil {
		return "", err
	}

	mt, payload, err := da.Payload()
	if err != nil {
		return "", err
	}

	revision, err := ah.blobStore.Put(ctx, mt, payload)
	if err != nil {
		dcontext.GetLogger(ctx).Errorf("error putting payload into blobstore: %v", err)
		return "", err
	}

	err = ah.linkManifests(ctx, *da, revision.Digest)
	if err != nil {
		dcontext.GetLogger(ctx).Errorf("error linking referrer metadata: %v", err)
		return "", err
	}

	return revision.Digest, nil
}

// verifyManifest ensures that the manifest content is valid from the
// perspective of the registry. As a policy, the registry only tries to
// store valid content, leaving trust policies of that content up to
// consumers.
func (ah *ociArtifactHandler) verifyManifest(ctx context.Context, da artifact.DeserializedArtifact, skipDependencyVerification bool) error {
	var errs distribution.ErrManifestVerification

	if da.ArtifactType() == "" {
		errs = append(errs, distribution.ErrArtifactTypeUnsupported{ArtifactType: ""})
	}

	if !skipDependencyVerification {
		blobStore := ah.repository.Blobs(ctx)

		// All references must exist.
		for _, blobDesc := range da.References() {
			desc, err := blobStore.Stat(ctx, blobDesc.Digest)
			if err != nil && err != distribution.ErrBlobUnknown {
				errs = append(errs, err)
			}
			if err != nil || desc.Digest == "" {
				// On error here, we always append unknown blob errors.
				errs = append(errs, distribution.ErrManifestBlobUnknown{Digest: blobDesc.Digest})
			}
		}

		manifestService, err := ah.repository.Manifests(ctx)
		if err != nil {
			return err
		}

		// Validate subject manifest.
		subject := da.SubjectManifest()
		exists, err := manifestService.Exists(ctx, subject.Digest)
		if !exists || err == distribution.ErrBlobUnknown {
			errs = append(errs, distribution.ErrManifestBlobUnknown{Digest: subject.Digest})
		} else if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) != 0 {
		return errs
	}

	return nil
}

func (ah *ociArtifactHandler) linkManifests(ctx context.Context, da artifact.DeserializedArtifact, revision digest.Digest) error {
	daArtifactType := da.ArtifactType()

	// Index referrer metadata for the subject.
	subject := da.SubjectManifest()
	if err := ah.referrersStoreFunc(subject.Digest, daArtifactType).linkBlob(ctx, distribution.Descriptor{Digest: revision}); err != nil {
		return err
	}

	return nil
}
