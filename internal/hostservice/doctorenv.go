package hostservice

// DoctorNotStartedOnEnv is set in a loophole's doctor_cmd environment by `yolo check` when the
// backend the next launch would use does NOT start that loophole's host service, and holds that
// backend's name ("container" for Apple Container). Unset or empty means the service runs on
// that backend, or that the caller (`yolo loopholes status`, which has no workspace) does not
// know the backend.
//
// It exists so a self-check's remedy can name a step that works there: the decision is the
// launch's own (run.HostServiceAdmittedOn), made once by the caller, and the self-check only
// reads the result rather than re-deriving which backend admits which service.
const DoctorNotStartedOnEnv = "YOLO_DOCTOR_SERVICE_NOT_STARTED_ON"
