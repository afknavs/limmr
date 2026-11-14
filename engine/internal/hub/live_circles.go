package hub

import "limmr/internal/model"

// LiveCircles is for projections. Mutations, migrations, restore and sync must
// read raw records so archived knowledge is retained.
func LiveCircles(r *Repo) ([]model.Circle, error) {
	xs, err := List[model.Circle](r, "circles")
	out := []model.Circle{}
	for _, c := range xs {
		if c.Live() {
			out = append(out, c)
		}
	}
	return out, err
}
