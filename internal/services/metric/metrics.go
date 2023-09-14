package metric

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/harness/gitness/internal/services/job"
	"github.com/harness/gitness/internal/store"
	"github.com/harness/gitness/types"
	"github.com/harness/gitness/version"
)

const jobType = "metric-collector"

type metricData struct {
	IP         string `json:"ip"`
	Hostname   string `json:"hostname"`
	Installer  string `json:"installed_by"`
	Installed  string `json:"installed_at"`
	Version    string `json:"version"`
	Users      int64  `json:"user_count"`
	Repos      int64  `json:"repo_count"`
	Pipelines  int64  `json:"pipeline_count"`
	Executions int64  `json:"execution_count"`
}

type Collector struct {
	hostname       string
	enabled        bool
	endpoint       string
	token          string
	userStore      store.PrincipalStore
	repoStore      store.RepoStore
	pipelineStore  store.PipelineStore
	executionStore store.ExecutionStore
	scheduler      *job.Scheduler
}

func (c *Collector) Register(ctx context.Context) {
	if !c.enabled {
		return
	}
	c.scheduler.AddRecurring(ctx, jobType, jobType, "0 0 * * *", time.Minute)
}

func (c *Collector) Handle(ctx context.Context, _ string, _ job.ProgressReporter) (string, error) {

	if !c.enabled {
		return "", nil
	}

	// get first available user
	users, err := c.userStore.ListUsers(ctx, &types.UserFilter{
		Page: 1,
		Size: 1,
	})
	if err != nil {
		return "", err
	}
	if len(users) == 0 {
		return "", nil
	}

	// total users in the system
	totalUsers, err := c.userStore.CountUsers(ctx, &types.UserFilter{})
	if err != nil {
		return "", fmt.Errorf("failed to get users total count: %w", err)
	}

	// total repos in the system
	totalRepos, err := c.repoStore.Count(ctx, 0, &types.RepoFilter{})
	if err != nil {
		return "", fmt.Errorf("failed to get repositories total count: %w", err)
	}

	// total pipelines in the system
	totalPipelines, err := c.pipelineStore.Count(ctx, 0, types.ListQueryFilter{})
	if err != nil {
		return "", fmt.Errorf("failed to get pipelines total count: %w", err)
	}

	// total executions in the system
	totalExecutions, err := c.executionStore.Count(ctx, 0)
	if err != nil {
		return "", fmt.Errorf("failed to get executions total count: %w", err)
	}

	data := metricData{
		Hostname:   c.hostname,
		Installer:  users[0].Email,
		Installed:  time.Unix(users[0].Created, 0).Format(time.DateTime),
		Version:    version.Version.String(),
		Users:      totalUsers,
		Repos:      totalRepos,
		Pipelines:  totalPipelines,
		Executions: totalExecutions,
	}

	buf := new(bytes.Buffer)
	err = json.NewEncoder(buf).Encode(data)
	if err != nil {
		return "", fmt.Errorf("failed to encode metric data: %w", err)
	}

	endpoint := fmt.Sprintf("%s?api_key=%s", c.endpoint, c.token)
	req, err := http.NewRequest("POST", endpoint, buf)
	if err != nil {
		return "", fmt.Errorf("failed to create a request for metric data to endpoint %s: %w", endpoint, err)
	}
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")

	res, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send metric data to endpoint %s: %w", endpoint, err)
	}

	res.Body.Close()

	return res.Status, nil
}

// httpClient should be used for HTTP requests. It
// is configured with a timeout for reliability.
var httpClient = &http.Client{
	Transport: &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSHandshakeTimeout: 30 * time.Second,
		DisableKeepAlives:   true,
	},
	Timeout: 1 * time.Minute,
}
