// Copyright 2022 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform Free Trial License
// that can be found in the LICENSE.md file for this repository.

package gitrpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"code.gitea.io/gitea/modules/git"
	"github.com/harness/gitness/internal/gitrpc/rpc"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	receivePack = "receive-pack"
)

var safeGitProtocolHeader = regexp.MustCompile(`^[0-9a-zA-Z]+=[0-9a-zA-Z]+(:[0-9a-zA-Z]+=[0-9a-zA-Z]+)*$`)

type smartHTTPService struct {
	rpc.UnimplementedSmartHTTPServiceServer
	adapter   gitAdapter
	reposRoot string
}

func newHTTPService(adapter gitAdapter, gitRoot string) (*smartHTTPService, error) {
	reposRoot := filepath.Join(gitRoot, repoSubdirName)
	if _, err := os.Stat(reposRoot); errors.Is(err, os.ErrNotExist) {
		if err = os.MkdirAll(reposRoot, 0o700); err != nil {
			return nil, err
		}
	}

	return &smartHTTPService{
		adapter:   adapter,
		reposRoot: reposRoot,
	}, nil
}

func (s *smartHTTPService) getFullPathForRepo(uid string) string {
	return filepath.Join(s.reposRoot, fmt.Sprintf("%s.%s", uid, gitRepoSuffix))
}

func (s *smartHTTPService) InfoRefs(
	r *rpc.InfoRefsRequest,
	stream rpc.SmartHTTPService_InfoRefsServer,
) error {
	environ := make([]string, 0)
	environ = append(os.Environ(), environ...)
	if r.GitProtocol != "" {
		environ = append(environ, "GIT_PROTOCOL="+r.GitProtocol)
	}

	repoPath := s.getFullPathForRepo(r.GetRepoUid())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	w := NewWriter(func(p []byte) error {
		return stream.Send(&rpc.InfoRefsResponse{Data: p})
	})

	cmd := &bytes.Buffer{}
	if err := git.NewCommand(ctx, r.GetService(), "--stateless-rpc", "--advertise-refs", ".").
		Run(&git.RunOpts{
			Env:    environ,
			Dir:    repoPath,
			Stdout: cmd,
		}); err != nil {
		return status.Errorf(codes.Internal, "InfoRefsUploadPack: cmd: %v", err)
	}
	if _, err := w.Write(packetWrite("# service=git-" + r.GetService() + "\n")); err != nil {
		return status.Errorf(codes.Internal, "InfoRefsUploadPack: pktLine: %v", err)
	}

	if _, err := w.Write([]byte("0000")); err != nil {
		return status.Errorf(codes.Internal, "InfoRefsUploadPack: flush: %v", err)
	}

	if _, err := io.Copy(w, cmd); err != nil {
		return status.Errorf(codes.Internal, "InfoRefsUploadPack: %v", err)
	}
	return nil
}

func (s *smartHTTPService) ServicePack(stream rpc.SmartHTTPService_ServicePackServer) error {
	ctx := stream.Context()
	// Get basic repo data
	req, err := stream.Recv()
	if err != nil {
		return err
	}
	// if client sends data as []byte raise error, needs reader
	if req.Data != nil {
		return status.Errorf(codes.InvalidArgument, "PostUploadPack(): non-empty Data")
	}

	if req.RepoUid == "" {
		return status.Errorf(codes.InvalidArgument, "PostUploadPack(): repository UID is missing")
	}

	repoPath := s.getFullPathForRepo(req.GetRepoUid())

	stdin := NewReader(func() ([]byte, error) {
		resp, streamErr := stream.Recv()
		return resp.GetData(), streamErr
	})

	stdout := NewWriter(func(p []byte) error {
		return stream.Send(&rpc.ServicePackResponse{Data: p})
	})

	return serviceRPC(ctx, stdin, stdout, req, repoPath)
}

func serviceRPC(ctx context.Context, stdin io.Reader, stdout io.Writer, req *rpc.ServicePackRequest, dir string) error {
	protocol := req.GetGitProtocol()
	service := req.GetService()
	principalID := req.GetPrincipalId()
	repoUID := req.GetRepoUid()

	environ := make([]string, 0)
	if service == receivePack && principalID != "" {
		environ = []string{
			EnvRepoUID + "=" + repoUID,
			EnvPusherID + "=" + principalID,
		}
	}
	// set this for allow pre-receive and post-receive execute
	environ = append(environ, "SSH_ORIGINAL_COMMAND="+service)

	if protocol != "" && safeGitProtocolHeader.MatchString(protocol) {
		environ = append(environ, "GIT_PROTOCOL="+protocol)
	}

	var (
		stderr bytes.Buffer
	)
	cmd := git.NewCommand(ctx, service, "--stateless-rpc", dir)
	cmd.SetDescription(fmt.Sprintf("%s %s %s [repo_path: %s]", git.GitExecutable, service, "--stateless-rpc", dir))
	err := cmd.Run(&git.RunOpts{
		Dir:               dir,
		Env:               append(os.Environ(), environ...),
		Stdout:            stdout,
		Stdin:             stdin,
		Stderr:            &stderr,
		UseContextTimeout: true,
	})
	if err != nil && err.Error() != "signal: killed" {
		log.Err(err).Msgf("Fail to serve RPC(%s) in %s: %v - %s", service, dir, err, stderr.String())
	}
	return err
}

func packetWrite(str string) []byte {
	s := strconv.FormatInt(int64(len(str)+4), 16)
	if len(s)%4 != 0 {
		s = strings.Repeat("0", 4-len(s)%4) + s
	}
	return []byte(s + str)
}
