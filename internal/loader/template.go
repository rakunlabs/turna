package loader

import "github.com/rakunlabs/turna/pkg/render"

// renderTemplate executes content as a mugo template with the given data.
//
// It unifies the loads templating with the rest of turna, which renders
// print output, service env/command, filters and server config with mugo.
func renderTemplate(content string, data any) ([]byte, error) {
	return render.ExecuteWithData(content, data)
}
