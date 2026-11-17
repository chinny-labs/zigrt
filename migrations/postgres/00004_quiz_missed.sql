-- A missed quiz problem brings that topic's review forward (plan 3.6): a new
-- mastery event kind, quiz_missed. Kept in lockstep with
-- migrations/sqlite/00004_quiz_missed.sql, which has to rebuild the table.

-- +goose Up
ALTER TABLE mastery_events DROP CONSTRAINT mastery_events_kind_check;
ALTER TABLE mastery_events ADD CONSTRAINT mastery_events_kind_check CHECK (kind IN ('lesson_started',
    'lesson_paused', 'lesson_passed', 'review_passed', 'review_failed', 'implicit_credit',
    'pulled_forward', 'reopened', 'placed', 'parent_override', 'quiz_missed'));

-- +goose Down
DELETE FROM mastery_events WHERE kind = 'quiz_missed';
ALTER TABLE mastery_events DROP CONSTRAINT mastery_events_kind_check;
ALTER TABLE mastery_events ADD CONSTRAINT mastery_events_kind_check CHECK (kind IN ('lesson_started',
    'lesson_paused', 'lesson_passed', 'review_passed', 'review_failed', 'implicit_credit',
    'pulled_forward', 'reopened', 'placed', 'parent_override'));
