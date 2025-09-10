package config

// GetLicenseToLeastPrivilegedProfileMapping converts the LicenseToLeastPrivilegedProfileMapping to a map[string]string.
func (c *Salesforce) GetLicenseToLeastPrivilegedProfileMapping() map[string]string {
	out := make(map[string]string, len(c.LicenseToLeastPrivilegedProfileMapping))
	for k, v := range c.LicenseToLeastPrivilegedProfileMapping {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}
