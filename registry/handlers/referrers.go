package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/docker/distribution"
	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/registry/api/errcode"
	v2 "github.com/docker/distribution/registry/api/v2"
	"github.com/gorilla/handlers"
	"github.com/opencontainers/go-digest"
)

// referrersDispatcher takes the request context and builds the
// appropriate handler for handling manifest referrer requests.
func referrersDispatcher(ctx *Context, r *http.Request) http.Handler {
	handler := &referrersHandler{
		Context: ctx,
	}
	reference := getReference(ctx)
	dgst, err := digest.Parse(reference)
	if err != nil {
		// We just have a tag
		handler.Tag = reference
	} else {
		handler.Digest = dgst
	}

	mhandler := handlers.MethodHandler{
		"GET": http.HandlerFunc(handler.Artifacts),
	}

	return mhandler
}

type referrersResponse struct {
	Digest    digest.Digest                        `json:"digest,omitempty"`
	Tag       string                               `json:"tag,omitempty"`
	Referrers []distribution.ManifestDigestAndData `json:"references"`
	NextLink  string                               `json:"nextLink"`
}

// referrersHandler handles http operations on image manifest referrers.
type referrersHandler struct {
	*Context

	// One of tag or digest gets set, depending on what is present in context.
	Tag    string
	Digest digest.Digest
}

// Artifacts fetches the list of manifest referrer artifacts, filtered by artifact type specified in the request.
func (mrmh *referrersHandler) Artifacts(w http.ResponseWriter, r *http.Request) {
	dcontext.GetLogger(mrmh).Debug("Artifacts")

	// This can be empty
	artifactType := r.FormValue("referenceType")

	if mrmh.Tag != "" {
		tags := mrmh.Repository.Tags(mrmh)
		desc, err := tags.Get(mrmh, mrmh.Tag)
		if err != nil {
			if _, ok := err.(distribution.ErrTagUnknown); ok {
				mrmh.Errors = append(mrmh.Errors, v2.ErrorCodeManifestUnknown.WithDetail(err))
			} else {
				mrmh.Errors = append(mrmh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
			}
			return
		}
		mrmh.Digest = desc.Digest
	}

	manifestService, err := mrmh.Repository.Manifests(mrmh)
	if err != nil {
		mrmh.Errors = append(mrmh.Errors, err)
		return
	}

	referrers, err := manifestService.Referrers(mrmh.Context, mrmh.Digest, artifactType)
	if err != nil {
		if _, ok := err.(distribution.ErrManifestUnknownRevision); ok {
			mrmh.Errors = append(mrmh.Errors, v2.ErrorCodeManifestUnknown.WithDetail(err))
		} else {
			mrmh.Errors = append(mrmh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
		}
		return
	}

	if referrers == nil {
		referrers = []distribution.ManifestDigestAndData{}
	}

	response := referrersResponse{Referrers: referrers, NextLink: "not implemented"}
	if mrmh.Tag != "" {
		response.Tag = mrmh.Tag
	} else {
		response.Digest = mrmh.Digest
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	if err = enc.Encode(response); err != nil {
		mrmh.Errors = append(mrmh.Errors, errcode.ErrorCodeUnknown.WithDetail(err))
		return
	}
}
