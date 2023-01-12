# Roadmap

The Argo Rollouts roadmap is maintained in Github Milestones on the Github repository.

## Release Cycle

### Schedule

These are the upcoming releases date estimates:

| Release | Release Planning Meeting | Release Candidate 1   | General Availability     |
|---------|--------------------------|-----------------------|--------------------------|
| v1.4    | TBD                      | Monday, Dec. 19, 2022 | Tuesday, Jan. 9, 2023    |
| v1.5    | Monday, Mar. 6, 2023     | Monday, Mar. 20, 2023 | Monday, Apr. 10, 2023    |
| v1.6    | Monday, Jun. 5, 2023     | Monday, Jun. 19, 2023 | Wednesday, Jul. 12, 2023 |
| v1.7    | Monday, Sep. 4, 2023     | Monday, Sep. 18, 2023 | Monday, Oct. 9, 2023     |

### Release Process

#### Minor Releases (e.g. 1.x.0)

A minor Argo Rollouts release occurs four times a year, once every three months. Each General Availability (GA) release is
preceded by several Release Candidates (RCs). The first RC is released three weeks before the scheduled GA date.

These are the approximate release dates:

* The first Monday of January
* The first Monday of April
* The first Monday of July
* The first Monday of October

Dates may be shifted slightly to accommodate holidays. Those shifts should be minimal.

#### Patch Releases (e.g. 1.4.x)

Argo Rollouts patch releases occur on an as-needed basis. Only the three most recent minor versions are eligible for patch
releases. Versions older than the three most recent minor versions are considered EOL and will not receive bug fixes or
security updates.

### Feature Acceptance Criteria

To be eligible for inclusion in a minor release, a new feature must meet the following criteria before the release’s RC
date.

If it is a large feature that involves significant design decisions, that feature must be described in a Proposal.

The feature PR must include:

* Tests (passing)
* Documentation
* If necessary, a note in the Upgrading docs for the planned minor release
* The PR must be reviewed, approved, and merged by an Approver.

If these criteria are not met by the RC date, the feature will be ineligible for inclusion in the RC series or GA for
that minor release. It will have to wait for the next minor release.
