package gitengine

import (
	"context"
	"strconv"
	"strings"
	"time"
)

func RemoteTips(ctx context.Context, path string) (map[string]string, error) {
	out, err := Run(ctx, path, 15*time.Second, "for-each-ref", "--format=%(refname)%09%(objectname)", "refs/remotes/")
	tips := map[string]string{}
	if err != nil {
		return tips, err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) == 2 && !strings.HasSuffix(fields[0], "/HEAD") {
			tips[fields[0]] = fields[1]
		}
	}
	return tips, nil
}
func HasNewRemoteCommits(ctx context.Context, path string, before map[string]string) (bool, error) {
	after, err := RemoteTips(ctx, path)
	if err != nil {
		return false, err
	}
	for ref, tip := range after {
		old := before[ref]
		if old == tip {
			continue
		}
		args := []string{"rev-list", "--count", tip}
		if old != "" {
			args = append(args, "--not", old)
		}
		out, err := Run(ctx, path, 15*time.Second, args...)
		if err != nil {
			return false, err
		}
		count, err := strconv.Atoi(out)
		if err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}
