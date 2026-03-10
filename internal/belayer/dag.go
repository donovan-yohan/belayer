package belayer

import "github.com/donovan-yohan/belayer/internal/model"

// DAG tracks climb dependencies within a problem and determines which climbs
// are ready to be spawned based on the completion status of their dependencies.
type DAG struct {
	goals    map[string]*model.Climb // climbID -> climb (DAG owns copies)
	children map[string][]string     // climbID -> list of climbIDs that depend on it
}

// BuildDAG constructs a DAG from a slice of climbs. Climbs are stored as copies
// so the DAG owns its data. The children map is a reverse-lookup from a climb
// to the climbs that depend on it.
func BuildDAG(goals []model.Climb) *DAG {
	d := &DAG{
		goals:    make(map[string]*model.Climb, len(goals)),
		children: make(map[string][]string),
	}

	// Store copies of each climb.
	for i := range goals {
		g := goals[i] // copy
		d.goals[g.ID] = &g
	}

	// Build the reverse-lookup: for each dependency, record the dependent climb.
	for _, g := range d.goals {
		for _, depID := range g.DependsOn {
			d.children[depID] = append(d.children[depID], g.ID)
		}
	}

	return d
}

// ReadyClimbs returns climbs whose status is pending and whose all DependsOn
// climbs have status complete. These are the climbs that can be spawned.
func (d *DAG) ReadyClimbs() []model.Climb {
	var ready []model.Climb
	for _, g := range d.goals {
		if g.Status != model.ClimbStatusPending {
			continue
		}
		if d.allDepsComplete(g) {
			ready = append(ready, *g)
		}
	}
	return ready
}

// allDepsComplete returns true if every climb in DependsOn has status complete.
func (d *DAG) allDepsComplete(g *model.Climb) bool {
	for _, depID := range g.DependsOn {
		dep, ok := d.goals[depID]
		if !ok || dep.Status != model.ClimbStatusComplete {
			return false
		}
	}
	return true
}

// MarkComplete sets the climb's status to complete in the DAG.
func (d *DAG) MarkComplete(climbID string) {
	if g, ok := d.goals[climbID]; ok {
		g.Status = model.ClimbStatusComplete
	}
}

// MarkFailed sets the climb's status to failed in the DAG.
func (d *DAG) MarkFailed(climbID string) {
	if g, ok := d.goals[climbID]; ok {
		g.Status = model.ClimbStatusFailed
	}
}

// MarkRunning sets the climb's status to running in the DAG.
func (d *DAG) MarkRunning(climbID string) {
	if g, ok := d.goals[climbID]; ok {
		g.Status = model.ClimbStatusRunning
	}
}

// MarkSpotting sets the climb's status to spotting in the DAG.
func (d *DAG) MarkSpotting(climbID string) {
	if g, ok := d.goals[climbID]; ok {
		g.Status = model.ClimbStatusSpotting
	}
}

// AllComplete returns true if every climb in the DAG has status complete.
func (d *DAG) AllComplete() bool {
	for _, g := range d.goals {
		if g.Status != model.ClimbStatusComplete {
			return false
		}
	}
	return true
}

// Get returns the climb by ID, or nil if not found.
func (d *DAG) Get(climbID string) *model.Climb {
	return d.goals[climbID]
}

// Climbs returns all climbs in the DAG.
func (d *DAG) Climbs() []*model.Climb {
	result := make([]*model.Climb, 0, len(d.goals))
	for _, g := range d.goals {
		result = append(result, g)
	}
	return result
}

// AddClimbs inserts new climbs into an existing DAG. Used for correction climbs
// from anchor redistribution.
func (d *DAG) AddClimbs(goals []model.Climb) {
	for i := range goals {
		g := goals[i]
		d.goals[g.ID] = &g
		for _, depID := range g.DependsOn {
			d.children[depID] = append(d.children[depID], g.ID)
		}
	}
}
