package setter

import "github.com/donovan-yohan/belayer/internal/model"

// DAG tracks goal dependencies within a task and determines which goals
// are ready to be spawned based on the completion status of their dependencies.
type DAG struct {
	goals    map[string]*model.Goal // goalID -> goal (DAG owns copies)
	children map[string][]string    // goalID -> list of goalIDs that depend on it
}

// BuildDAG constructs a DAG from a slice of goals. Goals are stored as copies
// so the DAG owns its data. The children map is a reverse-lookup from a goal
// to the goals that depend on it.
func BuildDAG(goals []model.Goal) *DAG {
	d := &DAG{
		goals:    make(map[string]*model.Goal, len(goals)),
		children: make(map[string][]string),
	}

	// Store copies of each goal.
	for i := range goals {
		g := goals[i] // copy
		d.goals[g.ID] = &g
	}

	// Build the reverse-lookup: for each dependency, record the dependent goal.
	for _, g := range d.goals {
		for _, depID := range g.DependsOn {
			d.children[depID] = append(d.children[depID], g.ID)
		}
	}

	return d
}

// ReadyGoals returns goals whose status is pending and whose all DependsOn
// goals have status complete. These are the goals that can be spawned.
func (d *DAG) ReadyGoals() []model.Goal {
	var ready []model.Goal
	for _, g := range d.goals {
		if g.Status != model.GoalStatusPending {
			continue
		}
		if d.allDepsComplete(g) {
			ready = append(ready, *g)
		}
	}
	return ready
}

// allDepsComplete returns true if every goal in DependsOn has status complete.
func (d *DAG) allDepsComplete(g *model.Goal) bool {
	for _, depID := range g.DependsOn {
		dep, ok := d.goals[depID]
		if !ok || dep.Status != model.GoalStatusComplete {
			return false
		}
	}
	return true
}

// MarkComplete sets the goal's status to complete in the DAG.
func (d *DAG) MarkComplete(goalID string) {
	if g, ok := d.goals[goalID]; ok {
		g.Status = model.GoalStatusComplete
	}
}

// MarkFailed sets the goal's status to failed in the DAG.
func (d *DAG) MarkFailed(goalID string) {
	if g, ok := d.goals[goalID]; ok {
		g.Status = model.GoalStatusFailed
	}
}

// MarkRunning sets the goal's status to running in the DAG.
func (d *DAG) MarkRunning(goalID string) {
	if g, ok := d.goals[goalID]; ok {
		g.Status = model.GoalStatusRunning
	}
}

// AllComplete returns true if every goal in the DAG has status complete.
func (d *DAG) AllComplete() bool {
	for _, g := range d.goals {
		if g.Status != model.GoalStatusComplete {
			return false
		}
	}
	return true
}

// Get returns the goal by ID, or nil if not found.
func (d *DAG) Get(goalID string) *model.Goal {
	return d.goals[goalID]
}

// Goals returns all goals in the DAG.
func (d *DAG) Goals() []*model.Goal {
	result := make([]*model.Goal, 0, len(d.goals))
	for _, g := range d.goals {
		result = append(result, g)
	}
	return result
}
