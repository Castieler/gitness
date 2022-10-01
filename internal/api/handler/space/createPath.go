// Copyright 2021 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform Free Trial License
// that can be found in the LICENSE.md file for this repository.

package space

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/harness/gitness/internal/api/handler/common"
	"github.com/harness/gitness/internal/api/render"
	"github.com/harness/gitness/internal/api/request"
	"github.com/harness/gitness/internal/guard"
	"github.com/harness/gitness/internal/store"
	"github.com/harness/gitness/types"
	"github.com/harness/gitness/types/check"
	"github.com/harness/gitness/types/enum"
	"github.com/rs/zerolog/hlog"
)

/*
 * Writes json-encoded path information to the http response body.
 */
func HandleCreatePath(guard *guard.Guard, spaceStore store.SpaceStore) http.HandlerFunc {
	return guard.Space(
		enum.PermissionSpaceEdit,
		false,
		func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			log := hlog.FromRequest(r)
			space, _ := request.SpaceFrom(ctx)
			principal, _ := request.PrincipalFrom(ctx)

			in := new(common.CreatePathRequest)
			err := json.NewDecoder(r.Body).Decode(in)
			if err != nil {
				render.BadRequestf(w, "Invalid request body: %s.", err)
				return
			}

			params := &types.PathParams{
				Path:      strings.ToLower(in.Path),
				CreatedBy: principal.ID,
				Created:   time.Now().UnixMilli(),
				Updated:   time.Now().UnixMilli(),
			}

			// validate path
			if err = check.PathParams(params, space.Path, true); err != nil {
				render.UserfiedErrorOrInternal(w, err)
				return
			}

			// TODO: ensure principal is authorized to create a path pointing to in.Path
			path, err := spaceStore.CreatePath(ctx, space.ID, params)
			if err != nil {
				log.Error().Err(err).Msg("Failed to create path for space.")

				render.UserfiedErrorOrInternal(w, err)
				return
			}

			render.JSON(w, http.StatusOK, path)
		})
}
