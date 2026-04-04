# Azure Deployment Plan

Status: Ready for Validation

Task

Make the face detection and recognition system optional for Azure deployments and disabled by default, while keeping local development behavior unchanged.

Scope

- Modify [infra/main.bicep](/Users/chris/Documents/repos/github.com/cbellee/photo-api/infra/main.bicep) to add a default-off feature flag for the face system.
- Gate Azure-only face resources behind that flag.
- Modify [infra/modules/stor.bicep](/Users/chris/Documents/repos/github.com/cbellee/photo-api/infra/modules/stor.bicep) so face-specific storage tables are only created when the flag is enabled.

Planned Changes

1. Add a `deployFaceSystem` boolean parameter with a default of `false`.
2. Derive a single `faceSystemEnabled` variable from `deployFaceSystem` and `faceApiContainerImage`.
3. Gate these Azure resources behind `faceSystemEnabled`:
   - Face container app
   - Face cron job
   - Table RBAC assignment
   - Face queue Dapr component
   - Photo API `FACE_STORE_TYPE` Azure setting
4. Update the storage module to create face tables only when requested.
5. Validate the edited Bicep files and report any pre-existing diagnostics separately.

Expected Outcome

- Azure deployments will not deploy the face detection and recognition stack unless explicitly enabled.
- Local development remains unchanged.
- Future Azure enablement will require setting `deployFaceSystem = true` and providing `faceApiContainerImage`.