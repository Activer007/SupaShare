package migration

import (
	"context"
	"fmt"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/configuration/database/dbabstraction"
	"github.com/Activer007/SupaShare/internal/environment/flagparser"
	"os"
)

// Do checks the passed flags for a migration and then executes it
func Do(flags flagparser.MigrateFlags) {
	oldDb, err := database.ParseUrl(flags.Source, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Invalid or unavailable migration source; verify the protected connection input")
		osExit(1)
		return
	}
	newDb, err := database.ParseUrl(flags.Destination, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Invalid or unavailable migration destination; verify the protected connection input")
		osExit(2)
		return
	}
	fmt.Printf("Migrating %s database to %s database\n", getType(oldDb.Type), getType(newDb.Type))
	if err := database.MigrateChecked(context.Background(), oldDb, newDb); err != nil {
		fmt.Fprintln(os.Stderr, "Migration rejected; check connectivity, destination emptiness and recovery prerequisites (connection details omitted)")
		osExit(3)
		return
	}
}

func getType(input int) string {
	switch input {
	case dbabstraction.TypeSqlite:
		return "SQLite"
	case dbabstraction.TypeRedis:
		return "Redis"
	}
	return "Invalid"
}

// Declared for testing
var osExit = os.Exit
