package gitplex

import "fmt"

func topoOrder(manifest Manifest) ([]string, error) {
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var order []string

	var visit func(string) error
	visit = func(name string) error {
		if visited[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("dependency cycle involving %q", name)
		}
		repo, ok := manifest.Repos[name]
		if !ok {
			return fmt.Errorf("unknown repo %q", name)
		}
		visiting[name] = true
		for dep := range repo.Dependencies {
			if _, ok := manifest.Repos[dep]; !ok {
				return fmt.Errorf("repo %q depends on unknown repo %q", name, dep)
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[name] = false
		visited[name] = true
		order = append(order, name)
		return nil
	}

	for name := range manifest.Repos {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return order, nil
}
