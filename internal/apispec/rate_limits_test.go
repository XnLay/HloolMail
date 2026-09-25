package apispec

import (
	"strings"
	"testing"
)

func TestAutomationRateLimitRetryContract(t *testing.T) {
	for _, operation := range AutomationOperations() {
		response := openAPIResponses(operation)["429"]
		header, ok := response.Headers["Retry-After"]
		if !ok || header.Schema["type"] != "integer" || header.Schema["minimum"] != 1 {
			t.Errorf("%s does not declare Retry-After in integer seconds", operation.ID)
		}
		if !strings.Contains(response.Description, "调用配额") {
			t.Errorf("%s does not distinguish quota from rate rejection", operation.ID)
		}
	}
	for _, operation := range registry {
		if operation.Auth != AuthAPIKey && openAPIResponses(operation)["429"].Headers != nil {
			t.Errorf("non-automation route %s claims configurable rate headers", operation.ID)
		}
	}
	for name, document := range map[string]string{"markdown": Markdown(Config{}), "skill": SkillMarkdown(Config{})} {
		if !strings.Contains(document, apiRateLimitGuide) {
			t.Errorf("%s omits the canonical retry guide", name)
		}
	}
}
