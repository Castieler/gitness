// Copyright 2022 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform Free Trial License
// that can be found in the LICENSE.md file for this repository.

package repo

import (
	"net/http"

	"github.com/harness/gitness/internal/api/controller/repo"
	"github.com/harness/gitness/internal/api/render"
	"github.com/harness/gitness/internal/api/request"
)

// HandleBlame returns the git blame output for a file.
func HandleBlame(repoCtrl *repo.Controller) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		session, _ := request.AuthSessionFrom(ctx)

		repoRef, err := request.GetRepoRefFromPath(r)
		if err != nil {
			render.TranslatedUserError(w, err)
			return
		}

		path := request.GetOptionalRemainderFromPath(r)

		// line_from is optional, skipped if set to 0
		lineFrom, err := request.QueryParamAsPositiveInt64OrDefault(r, request.QueryParamLineFrom, 0)
		if err != nil {
			render.TranslatedUserError(w, err)
			return
		}

		// line_to is optional, skipped if set to 0
		lineTo, err := request.QueryParamAsPositiveInt64OrDefault(r, request.QueryParamLineTo, 0)
		if err != nil {
			render.TranslatedUserError(w, err)
			return
		}

		gitRef := request.GetGitRefFromQueryOrDefault(r, "")

		stream, err := repoCtrl.Blame(ctx, session, repoRef, gitRef, path, int(lineFrom), int(lineTo))
		if err != nil {
			render.TranslatedUserError(w, err)
			return
		}

		render.JSONArrayDynamic(ctx, w, stream)
	}
}
