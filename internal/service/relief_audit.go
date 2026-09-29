package service

import "github.com/wyw14/solo-039/internal/relief"

func reliefCertExpiry(cert relief.Certificate, present bool) any {
	if !present {
		return nil
	}
	return cert.ExpiresAt
}

func reliefUnavailableReason(a relief.Availability) string {
	if a.Available {
		return ""
	}
	return string(a.Reason)
}

func reliefActiveDisposition(a relief.Availability) any {
	if !a.DispoPresent {
		return nil
	}
	return a.Disposition
}
