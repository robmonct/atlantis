package events

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/hootsuite/atlantis/server/events/github"
	"github.com/hootsuite/atlantis/server/events/locking"
	"github.com/hootsuite/atlantis/server/events/models"
	"github.com/pkg/errors"
)

type PullClosedExecutor struct {
	Locker    locking.Locker
	Github    github.Client
	Workspace Workspace
}

type templatedProject struct {
	Path string
	Envs string
}

var pullClosedTemplate = template.Must(template.New("").Parse(
	"Locks and plans deleted for the projects and environments modified in this pull request:\n" +
		"{{ range . }}\n" +
		"- path: `{{ .Path }}` {{ .Envs }}{{ end }}"))

func (p *PullClosedExecutor) CleanUpPull(repo models.Repo, pull models.PullRequest) error {
	// delete the workspace
	if err := p.Workspace.Delete(repo, pull); err != nil {
		return errors.Wrap(err, "cleaning workspace")
	}

	// finally, delete locks. We do this last because when someone
	// unlocks a project, right now we don't actually delete the plan
	// so we might have plans laying around but no locks
	locks, err := p.Locker.UnlockByPull(repo.FullName, pull.Num)
	if err != nil {
		return errors.Wrap(err, "cleaning up locks")
	}

	// if there are no locks then there's no need to comment
	if len(locks) == 0 {
		return nil
	}

	templateData := p.buildTemplateData(locks)
	var buf bytes.Buffer
	if err = pullClosedTemplate.Execute(&buf, templateData); err != nil {
		return errors.Wrap(err, "rendering template for comment")
	}
	return p.Github.CreateComment(repo, pull, buf.String())
}

// buildTemplateData formats the lock data into a slice that can easily be templated
// for the GitHub comment. We organize all the environments by their respective project paths
// so the comment can look like: path: {path}, environments: {all-envs}
func (p *PullClosedExecutor) buildTemplateData(locks []models.ProjectLock) []templatedProject {
	envsByPath := make(map[string][]string)
	for _, l := range locks {
		path := l.Project.RepoFullName + "/" + l.Project.Path
		envsByPath[path] = append(envsByPath[path], l.Env)
	}

	var projects []templatedProject
	for p, e := range envsByPath {
		envsStr := fmt.Sprintf("`%s`", strings.Join(e, "`, `"))
		if len(e) == 1 {
			projects = append(projects, templatedProject{
				Path: p,
				Envs: "environment: " + envsStr,
			})
		} else {
			projects = append(projects, templatedProject{
				Path: p,
				Envs: "environments: " + envsStr,
			})

		}
	}
	return projects
}
