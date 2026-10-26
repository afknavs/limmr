package efforts

import (
	"fmt"
	"testing"
)

func TestPersonFilterPrecedesResultLimit(t *testing.T) {
	c := cached{}
	for i := 0; i < 501; i++ {
		v := Effort{ID: fmt.Sprint(i), Title: "Anatomy practical syllabus", Status: "moving"}
		if i == 500 {
			v.People = []Entity{{ID: "maya", Name: "Maya Rao"}}
		}
		c.efforts = append(c.efforts, v)
	}
	result := filterSnapshot(c, Filter{Person: "maya", Limit: 1})
	if len(result) != 1 || result[0].ID != "500" {
		t.Fatal("person effort beyond first 500 omitted")
	}
	if result := filterSnapshot(c, Filter{Person: "maya", Status: "done", Limit: 1}); len(result) != 0 {
		t.Fatal("person filter bypassed status")
	}
}
