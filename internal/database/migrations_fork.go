// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package database // import "miniflux.app/v2/internal/database"

import (
	"database/sql"
)

// forkMigrations contains the migrations that are specific to this fork.
//
// They are applied after all upstream migrations (see migrations.go), which
// keeps migrations.go byte-identical to upstream and avoids conflicts when
// rebasing on a new Miniflux release.
//
// Order matters: always append new fork migrations at the end of the list.
var forkMigrations = []func(tx *sql.Tx) error{
	func(tx *sql.Tx) (err error) {
		// entries.content used to hold two different things: the content
		// provided by the feed, then the scraped web page content when the
		// crawler is enabled (the scraped content overwrote the feed one).
		//
		// entries.summary now keeps the original feed content whenever the
		// scraper replaced entries.content. An empty summary means that
		// entries.content still comes from the feed itself.
		//
		// Existing rows are not backfilled on purpose: for those rows it is
		// impossible to know whether content comes from the feed or from the
		// scraper.
		//
		// The statements below are idempotent: the column may already exist
		// because it was created manually before this migration was written.
		// In that case we only make sure the default value and the NOT NULL
		// constraint match what the application expects, instead of failing
		// with SQLSTATE 42701 (duplicate_column).
		_, err = tx.Exec(`ALTER TABLE entries ADD COLUMN IF NOT EXISTS summary text;`)
		if err != nil {
			return err
		}

		_, err = tx.Exec(`UPDATE entries SET summary='' WHERE summary IS NULL;`)
		if err != nil {
			return err
		}

		_, err = tx.Exec(`ALTER TABLE entries ALTER COLUMN summary SET DEFAULT '';`)
		if err != nil {
			return err
		}

		_, err = tx.Exec(`ALTER TABLE entries ALTER COLUMN summary SET NOT NULL;`)
		return err
	},
	func(tx *sql.Tx) (err error) {
		// Per-feed Cloudflare bypass override:
		// NULL = follow global config; true/false = force enable/disable.
		// IF NOT EXISTS keeps the migration idempotent if the column was
		// created manually before this migration landed.
		_, err = tx.Exec(`ALTER TABLE feeds ADD COLUMN IF NOT EXISTS cloudflare_bypass boolean;`)
		return err
	},
}

// allMigrations is the complete ordered list of migrations: upstream ones
// first, then the fork-specific ones.
var allMigrations = append(migrations[:], forkMigrations...)

// allSchemaVersion is the expected schema version of this fork.
var allSchemaVersion = len(allMigrations)
