DELETE FROM orchestrator_transition WHERE state = 'shadow';
ALTER TABLE orchestrator_transition DROP CONSTRAINT IF EXISTS orchestrator_transition_state_check;
ALTER TABLE orchestrator_transition ADD CONSTRAINT orchestrator_transition_state_check CHECK (state IN ('started', 'done', 'failed'));
