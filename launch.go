package main

import (
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/apex/log"
	clihander "github.com/apex/log/handlers/cli"
	"github.com/blacktop/lporg/database"
	"github.com/blacktop/lporg/database/utils"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	yaml "gopkg.in/yaml.v2"
)

var (
	// Version stores the plugin's version
	Version string
	// BuildTime stores the plugin's build time
	BuildTime string
	// for log output
	bold = "\033[1m%s\033[0m"
	// lpad is the main object
	lpad database.LaunchPad
)

// CmdDefaultOrg will organize your launchpad by the app default categories
func CmdDefaultOrg(verbose bool) error {
	log.Info("IMPLIMENT DEFAULT ORG HERE <=================")
	return nil
}

func parsePages(root int, parentMapping map[int][]database.Item) database.Apps {
	var apps database.Apps

	for pageNum, page := range parentMapping[root] {

		log.Infof("page number: %d", pageNum+1)

		p := database.Page{Number: pageNum + 1}
		f := database.Folder{}
		fp := database.FolderPage{}

		for _, item := range parentMapping[page.ID] {
			switch item.Type {
			case database.ApplicationType:
				utils.Indent(log.WithField("title", item.App.Title).Info)("found app")
				p.FlatItems = append(p.FlatItems, item.App.Title)
			case database.WidgetType:
				utils.Indent(log.WithField("title", item.Widget.Title).Info)("found widget")
				p.FlatItems = append(p.FlatItems, item.Widget.Title)
			case database.FolderRootType:
				utils.Indent(log.WithField("title", item.Group.Title).Info)("found folder")
				f.Name = item.Group.Title
				fp.Number = 1
				folderPageItem := parentMapping[item.ID][0]
				for _, folder := range parentMapping[folderPageItem.ID] {
					utils.DoubleIndent(log.WithField("title", folder.App.Title).Info)("found app")
					fp.Items = append(fp.Items, folder.App.Title)
				}
				f.Pages = append(f.Pages, fp)
			case database.PageType:
				utils.Indent(log.WithField("", item.Group.Title).Info)("found page")
			default:
				utils.Indent(log.WithField("type", item.Type).Error)("found ?")
			}
		}

		if len(f.Pages) > 0 {
			p.Folders = append(p.Folders, f)
		}
		apps.Pages = append(apps.Pages, p)
	}
	return apps
}

// CmdSaveConfig will save your launchpad settings to a config file
func CmdSaveConfig(verbose bool) error {

	log.Infof(bold, "SAVING LAUNCHPAD DATABASE")

	if verbose {
		log.SetLevel(log.DebugLevel)
	}

	var (
		launchpadRoot       int
		dashboardRoot       int
		launchpadRootPageID int
		dashboardRootPageID int
		items               []database.Item
		dbinfo              []database.DBInfo
		conf                database.Config
	)

	// find launchpad database
	tmpDir := os.Getenv("TMPDIR")
	lpad.Folder = filepath.Join(tmpDir, "../0/com.apple.dock.launchpad/db")
	lpad.File = filepath.Join(lpad.Folder, "db")
	lpad.File = "./launchpad.db"
	if _, err := os.Stat(lpad.File); os.IsNotExist(err) {
		utils.Indent(log.WithError(err).WithField("path", lpad.File).Fatal)("launchpad DB not found")
	}
	utils.Indent(log.WithFields(log.Fields{"database": lpad.File}).Info)("found launchpad database")

	// open launchpad database
	db, err := gorm.Open("sqlite3", lpad.File)
	if err != nil {
		return err
	}
	defer db.Close()

	if verbose {
		db.LogMode(true)
	}

	// get launchpad and dashboard roots
	if err := db.Where("key in (?)", []string{"launchpad_root", "dashboard_root"}).Find(&dbinfo).Error; err != nil {
		log.WithError(err).Error("dbinfo query failed")
	}
	for _, info := range dbinfo {
		switch info.Key {
		case "launchpad_root":
			launchpadRoot, _ = strconv.Atoi(info.Value)
		case "dashboard_root":
			dashboardRoot, _ = strconv.Atoi(info.Value)
		default:
			log.WithField("key", info.Key).Error("bad key")
		}
	}

	if err := db.Not("uuid in (?)", []string{"ROOTPAGE", "HOLDINGPAGE", "ROOTPAGE_DB", "HOLDINGPAGE_DB", "ROOTPAGE_VERS", "HOLDINGPAGE_VERS"}).
		Order("items.parent_id, items.ordering").
		Find(&items).Error; err != nil {
		log.WithError(err).Error("items query failed")
	}

	// create parent mapping object
	log.Info("collecting launchpad/dashboard pages")
	parentMapping := make(map[int][]database.Item)
	for _, item := range items {
		db.Model(&item).Related(&item.App)
		db.Model(&item).Related(&item.Widget)
		db.Model(&item).Related(&item.Group)

		if item.ParentID == launchpadRoot {
			launchpadRootPageID = item.ID
			utils.Indent(log.WithField("id", launchpadRootPageID).Info)("launchpad page found")
		}

		if item.ParentID == dashboardRoot {
			dashboardRootPageID = item.ID
			utils.Indent(log.WithField("id", dashboardRootPageID).Info)("dashboard page found")
		}

		parentMapping[item.ParentID] = append(parentMapping[item.ParentID], item)
	}

	log.Info("interating over launchpad pages")
	conf.Apps = parsePages(launchpadRoot, parentMapping)

	log.Info("interating over dashboard pages")
	conf.Widgets = parsePages(dashboardRoot, parentMapping)

	// write out config YAML file
	d, err := yaml.Marshal(&conf)
	if err != nil {
		return errors.Wrap(err, "unable to marshall YAML")
	}

	if err = ioutil.WriteFile("launchpad-save.yaml", d, 0644); err != nil {
		return errors.Wrap(err, "unable to write YAML")
	}

	log.Infof(bold, strings.ToUpper("successfully wrote launchpad.yaml"))

	return nil
}

// CmdLoadConfig will load your launchpad settings from a config file
func CmdLoadConfig(verbose bool, configFile string) error {

	log.Infof(bold, "PARSE LAUCHPAD DATABASE")

	if verbose {
		log.SetLevel(log.DebugLevel)
	}

	// Older macOS ////////////////////////////////
	// $HOME/Library/Application\ Support/Dock/*.db

	// High Sierra //////////////////////////////
	// $TMPDIR../0/com.apple.dock.launchpad/db/db

	// find launchpad database
	tmpDir := os.Getenv("TMPDIR")
	lpad.Folder = filepath.Join(tmpDir, "../0/com.apple.dock.launchpad/db")
	lpad.File = filepath.Join(lpad.Folder, "db")
	// lpad.File = "./launchpad.db"
	if _, err := os.Stat(lpad.File); os.IsNotExist(err) {
		utils.Indent(log.WithError(err).WithField("path", lpad.File).Fatal)("launchpad DB not found")
	}
	utils.Indent(log.WithFields(log.Fields{"database": lpad.File}).Info)("found launchpad database")

	// start from a clean slate
	err := removeOldDatabaseFiles(lpad.Folder)
	if err != nil {
		return err
	}

	// open launchpad database
	db, err := gorm.Open("sqlite3", lpad.File)
	if err != nil {
		return err
	}
	defer db.Close()

	lpad.DB = db

	if verbose {
		db.LogMode(true)
	}

	// Disable the update triggers
	if err := lpad.DisableTriggers(); err != nil {
		log.WithError(err).Fatal("DisableTriggers failed")
	}

	// Clear all items related to groups so we can re-create them
	if err := lpad.ClearGroups(); err != nil {
		log.WithError(err).Fatal("ClearGroups failed")
	}
	// Add root and holding pages to items and groups
	if err := lpad.AddRootsAndHoldingPages(); err != nil {
		log.WithError(err).Fatal("AddRootsAndHoldingPagesfailed")
	}

	// We will begin our group records using the max ids found (groups always appear after apps and widgets)
	groupID := int(math.Max(float64(lpad.GetMaxAppID()), float64(lpad.GetMaxWidgetID())))

	// Read in Config file
	config, err := database.LoadConfig(configFile)
	if err != nil {
		log.WithError(err).Fatal("database.LoadConfig")
	}

	////////////////////////////////////////////////////////////////////
	// Place Widgets ///////////////////////////////////////////////////
	utils.Indent(log.Info)("creating Widget folders and adding widgets to them")
	missing, err := lpad.GetMissing(config.Widgets, database.WidgetType)
	if len(missing) > 0 {
		p := database.Page{
			Number: len(config.Widgets.Pages) + 1,
		}
		p.FlatItems = missing
		utils.DoubleIndent(log.Info)("missing apps will be added to the last page")
		config.Widgets.Pages = append(config.Widgets.Pages, p)
	}
	groupID, err = lpad.ApplyConfig(config.Widgets, database.WidgetType, groupID, 3)
	if err != nil {
		log.WithError(err).Fatal("ApplyConfig=>Widgets")
	}

	/////////////////////////////////////////////////////////////////////
	// Place Apps ///////////////////////////////////////////////////////
	utils.Indent(log.Info)("creating App folders and adding apps to them")
	missing, err = lpad.GetMissing(config.Apps, database.ApplicationType)
	if len(missing) > 0 {
		p := database.Page{
			Number: len(config.Apps.Pages) + 1,
		}
		p.FlatItems = missing
		utils.DoubleIndent(log.Info)("missing apps will be added to the last page")
		config.Apps.Pages = append(config.Apps.Pages, p)
	}
	groupID, err = lpad.ApplyConfig(config.Apps, database.ApplicationType, groupID, 1)
	if err != nil {
		log.WithError(err).Fatal("ApplyConfig=>Apps")
	}

	// Re-enable the update triggers
	if err := lpad.EnableTriggers(); err != nil {
		log.WithError(err).Fatal("EnableTriggers failed")
	}

	return restartDock()
}

func init() {
	log.SetHandler(clihander.Default)
}

var appHelpTemplate = `Usage: {{.Name}} {{if .Flags}}[OPTIONS] {{end}}COMMAND [arg...]

{{.Usage}}

Version: {{.Version}}{{if or .Author .Email}}
Author:{{if .Author}} {{.Author}}{{if .Email}} - <{{.Email}}>{{end}}{{else}}
  {{.Email}}{{end}}{{end}}
{{if .Flags}}
Options:
  {{range .Flags}}{{.}}
  {{end}}{{end}}
Commands:
  {{range .Commands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
  {{end}}
Run '{{.Name}} COMMAND --help' for more information on a command.
`

func main() {

	cli.AppHelpTemplate = appHelpTemplate
	app := cli.NewApp()

	app.Name = "lporg"
	app.Author = "blacktop"
	app.Email = "https://github.com/blacktop"
	app.Version = Version + ", BuildTime: " + BuildTime
	app.Compiled, _ = time.Parse("20060102", BuildTime)
	app.Usage = "Organize Your Launchpad"
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "verbose, V",
			Usage: "verbose output",
		},
	}
	app.Commands = []cli.Command{
		{
			Name:  "default",
			Usage: "organize by default app categories",
			Action: func(c *cli.Context) error {
				fmt.Println(porg)
				return CmdDefaultOrg(c.GlobalBool("verbose"))
			},
		},
		{
			Name:  "save",
			Usage: "save current launchpad settings",
			Action: func(c *cli.Context) error {
				return CmdSaveConfig(c.GlobalBool("verbose"))
			},
		},
		{
			Name:  "load",
			Usage: "load launchpad settings config from `FILE`",
			Action: func(c *cli.Context) error {
				if c.Args().Present() {
					// user supplied launchpad config YAML
					err := CmdLoadConfig(c.GlobalBool("verbose"), c.Args().First())
					if err != nil {
						return err
					}
				} else {
					log.Fatal("please supply a config file to load")
				}
				return nil
			},
		},
	}
	app.Action = func(c *cli.Context) error {
		if !c.Args().Present() {
			cli.ShowAppHelp(c)
		}
		return nil
	}

	if err := app.Run(os.Args); err != nil {
		log.WithError(err).Fatal("failed")
	}
}
