package handler

import (

)

// TestClaimTaskByRuntime_LegacySkillRedirectFollowsTheCapability covers the
// wiring the two end-to-end tests leave out (MUL-6986).
//
// The service test drives the decision by passing a bool straight in, and the
// brief test hand-builds a skill set. Both ends were covered while the middle —
// X-Client-Capabilities on the claim request, through the handler, into the
// payload the daemon actually receives — was not. If that link ever breaks, the
// other two tests stay green: an old daemon would silently lose its redirect
// and its brief would point at a skill nobody shipped, or every current daemon
// would start paying for a stub it does not need.
//
// Both claim branches are covered because they compute the built-in set
// separately: the slim skill-refs claim and the older inline-skills claim.
