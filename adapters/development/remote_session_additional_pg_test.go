package development

import "testing"

func TestConfiguredRemoteChildClosedBeforePrepareKeepsOriginalSessionPostgresParent(t *testing.T) {
	verifyConfiguredRemoteChildClosedBeforePrepareKeepsOriginalSession(t, "postgres")
}
func TestConfiguredRemoteSessionLostReplyRecoversOriginalCommandPostgresParent(t *testing.T) {
	verifyConfiguredRemoteSessionLostReplyRecoversOriginalCommand(t, "postgres")
}
func TestConfiguredRemoteChildAnswersOriginalRequestAndPreservesSubmissionPostgresParent(t *testing.T) {
	verifyConfiguredRemoteChildAnswersOriginalRequestAndPreservesSubmission(t, "postgres")
}
func TestConfiguredRemoteChildContinueSteersOriginalGoalAndPreservesSubmissionPostgresParent(t *testing.T) {
	verifyConfiguredRemoteChildContinueSteersOriginalGoalAndPreservesSubmission(t, "postgres")
}

func TestConfiguredRemoteChildStaleRequestSettlesOriginalParentCommandPostgresParent(t *testing.T) {
	verifyConfiguredRemoteChildStaleRequestSettlesOriginalParentCommand(t, "postgres")
}
