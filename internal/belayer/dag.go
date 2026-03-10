package belayer

import "github.com/donovan-yohan/belayer/internal/model"

// DAG tracks climb dependencies within a problem and determines which climbs
// are ready to be spawned based on the completion status of their dependencies.
type DAG struct {
	climbs   map[string]*model.Climb // climbID -> climb (DAG owns copies)
	children map[string][]string     // climbID -> list of climbIDs that depend on it
}

// BuildDAG constructs a DAG from a slice of climbs. Climbs are stored as copies
// so the DAG owns its data. The children map is a reverse-lookup from a climb
// to the climbs that depend on it.
func BuildDAG(climbs []model.Climb) *DAG {
	d := &DAG{
		climbs:   make(map[string]*model.Climb, len(climbs)),
		children: make(map[string][]string),
	}

	// Store copies of each climb.
	for i := range climbs {
		c := climbs[i] // copy
		d.climbs[c.ID] = &c
	}

	// Build the reverse-lookup: for each dependency, record the dependent climb.
	for _, c := range d.climbs {
		for _, depID := range c.DependsOn {
			d.children[depID] = append(d.children[depID], c.ID)
		}
	}

	return d
}

// ReadyClimbs returns climbs whose status is pending and whose all DependsOn
// climbs have status complete. These are the climbs that can be spawned.
func (d *DAG) ReadyClimbs() []model.Climb {
	var ready []model.Climb
	for _, c := range d.climbs {
		if c.Status != model.ClimbStatusPending {
			continue
		}
		if d.allDepsComplete(c) {
			ready = append(ready, *c)
		}
	}
	return ready
}

// allDepsComplete returns true if every climb in DependsOn has status complete.
func (d *DAG) allDepsComplete(c *model.Climb) bool {
	for _, depID := range c.DependsOn {
		dep, ok := d.climbs[depID]
		if !ok || dep.Status != model.ClimbStatusComplete {
			return false
		}
	}
	return true
}

// MarkComplete sets the climb's status to complete in the DAG.
func (d *DAG) MarkComplete(climbID string) {
	if c, ok := d.climbs[climbID]; ok {
		c.Status = model.ClimbStatusComplete
	}
}

// MarkFailed sets the climb's status to failed in the DAG.
func (d *DAG) MarkFailed(climbID string) {
	if c, ok := d.climbs[climbID]; ok {
		c.Status = model.ClimbStatusFailed
	}
}

// MarkRunning sets the climb's status to running in the DAG.
func (d *DAG) MarkRunning(climbID string) {
	if c, ok := d.climbs[climbID]; ok {
		c.Status = model.ClimbStatusRunning
	}
}

// AllComplete returns true if every climb in the DAG has status complete.
func (d *DAG) AllComplete() bool {
	for _, c := range d.climbs {
		if c.Status != model.ClimbStatusComplete {
			return false
		}
	}
	return true
}

// Get returns the climb by ID, or nil if not found.
func (d *DAG) Get(climbID string) *model.Climb {
	return d.climbs[climbID]
}

// Climbs returns all climbs in the DAG.
func (d *DAG) Climbs() []*model.Climb {
	result := make([]*model.Climb, 0, len(d.climbs))
	for _, c := range d.climbs {
		result = append(result, c)
	}
	return result
}

// AddClimbs inserts new climbs into an existing DAG. Used for correction climbs
// from anchor redistribution.
func (d *DAG) AddClimbs(climbs []model.Climb) {
	for i := range climbs {
		c := climbs[i]
		d.climbs[c.ID] = &c
		for _, depID := range c.DependsOn {
			d.children[depID] = append(d.children[depID], c.ID)
		}
	}
}

// AllClimbsForRepoTopped returns true if every climb for the repo has finished
// (not pending, not running, not failed). Used to detect when all climbs for a
// repo have topped so the per-repo spotter can be activated.
func (d *DAG) AllClimbsForRepoTopped(repoName string) bool {
	found := false
	for _, c := range d.climbs {
		if c.RepoName == repoName {
			found = true
			if c.Status == model.ClimbStatusPending || c.Status == model.ClimbStatusRunning || c.Status == model.ClimbStatusFailed {
				return false
			}
		}
	}
	return found
}

// ClimbsForRepo returns all climbs assigned to the given repo.
func (d *DAG) ClimbsForRepo(repoName string) []*model.Climb {
	var result []*model.Climb
	for _, c := range d.climbs {
		if c.RepoName == repoName {
			result = append(result, c)
		}
	}
	return result
}

// UniqueRepos returns all unique repo names in the DAG.
func (d *DAG) UniqueRepos() []string {
	repos := make(map[string]bool)
	for _, c := range d.climbs {
		repos[c.RepoName] = true
	}
	var result []string
	for r := range repos {
		result = append(result, r)
	}
	return result
}
