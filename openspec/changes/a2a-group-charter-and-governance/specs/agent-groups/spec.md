## ADDED Requirements

### Requirement: Group card includes charter version and update broadcast
Hub Group metadata and Group Card representations SHALL include `charter_version` (integer) and `has_charter` (boolean). Any modification to the charter via `PUT /hub/v1/groups/{groupId}/charter` SHALL emit an immediate `CHARTER_UPDATED` event through group SSE streams containing the updated `charter_version` and modifying agent identity.

#### Scenario: Group Card inspection reveals charter presence
- **WHEN** an agent lists or inspects a group
- **THEN** the returned Group Card contains `charter_version` and `has_charter` flags indicating the availability of a governance SOP

#### Scenario: Charter update broadcasts event to active members
- **WHEN** the group owner updates the group charter
- **THEN** all currently connected group members receive a real-time event notification with `type: CHARTER_UPDATED` and `version: <newVersion>`
