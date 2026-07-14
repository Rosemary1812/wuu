package agentcontrol

import "fmt"

const ultraRootPolicyTemplate = `Ultra mode is active. Proactive delegation is now the default: earlier
guidance telling you to keep work local unless delegation materially
improves the result no longer applies. Spawn subagents whenever parallel
work or a separate context would improve speed or quality, and decompose
deep or wide tasks into a tree of bounded subtasks - child agents can
spawn their own subagents. Keep the blocking step on your critical path
local, and delegate the rest. You have %d concurrent agent slots; additional
agents queue safely. Do not duplicate work across agents. Once delegated,
trust the child to finish its assignment, and use send_message or close_agent
only when new information or changed scope requires intervention.`

const ultraWorkerPolicy = `You are part of a team of agents. You may spawn subagents for bounded
subtasks (spawn_agent), message them (send_message), and close the ones
you no longer need (close_agent). Keep the blocking step on your critical
path local; delegate independent or context-heavy work. Child results
arrive automatically - do not poll. Before finishing, ensure your child
tasks have completed or close the ones you no longer need, then integrate
their results into your own final handoff.`

// UltraRootPolicy returns the request-only policy for an Ultra root turn.
// The P2 recursion layer provides the policy next to the worker policy so the
// two capability promises cannot drift; the root turn injector wires it in.
func UltraRootPolicy(maxParallel int) string {
	return fmt.Sprintf(ultraRootPolicyTemplate, maxParallel)
}

// UltraWorkerPolicy returns the shared policy used by both fresh and forked
// Ultra workers.
func UltraWorkerPolicy() string {
	return ultraWorkerPolicy
}
