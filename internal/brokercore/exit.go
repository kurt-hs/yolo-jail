package brokercore

// The exit codes a jail sees from every broker source, sysexits(3) where one fits.
const (
	ExitUsage       = 64 // EX_USAGE: refused, or out of scope
	ExitUnavailable = 69 // EX_UNAVAILABLE: no broker, no host program, or no host login
	ExitTempFail    = 75 // EX_TEMPFAIL: pending a human (step 2)
	ExitNoPerm      = 77 // EX_NOPERM: denied, or a write this version cannot ask for (at once: BB-D66)
	ExitTimeout     = 124
)
