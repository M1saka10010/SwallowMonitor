package store

const (
	settingSiteName        = "siteName"
	settingSiteDescription = "siteDescription"
	defaultSiteName        = "SwallowMonitor"
)

// SiteSettings holds user-editable website settings.
type SiteSettings struct {
	SiteName        string `json:"siteName"`
	SiteDescription string `json:"siteDescription"`
}

// GetSiteSettings returns stored settings with defaults for missing values.
func (s *Store) GetSiteSettings() (SiteSettings, error) {
	settings := SiteSettings{SiteName: defaultSiteName}
	rows, err := s.db.Query(`SELECT key, value FROM settings WHERE key IN (?, ?)`, settingSiteName, settingSiteDescription)
	if err != nil {
		return settings, err
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return settings, err
		}
		switch key {
		case settingSiteName:
			if value != "" {
				settings.SiteName = value
			}
		case settingSiteDescription:
			settings.SiteDescription = value
		}
	}
	return settings, rows.Err()
}

// UpdateSiteSettings persists user-editable website settings.
func (s *Store) UpdateSiteSettings(settings SiteSettings) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, settingSiteName, settings.SiteName); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, settingSiteDescription, settings.SiteDescription); err != nil {
		return err
	}
	return tx.Commit()
}
