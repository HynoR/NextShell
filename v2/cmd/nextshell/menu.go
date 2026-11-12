package main

import "github.com/egoist/mygo"

// appMenu is the menu bar. It leaves out File (Close Window, Cmd+W) and View's Reload and DevTools:
// a menu item's key goes to the menu before the window, and Cmd+W closes a tab in appui, not the window.
func appMenu(showSettings func()) *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			mygo.Separator(),
			{Label: "Settings…", Accelerator: "Cmd+,", Click: func(*mygo.MenuItem, *mygo.Window) { showSettings() }},
			mygo.Separator(),
			{Role: mygo.RoleServices},
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Role: mygo.RoleEditMenu},
		{Label: "View", Submenu: []*mygo.MenuItem{{Role: mygo.RoleToggleFullScreen}}},
		{Role: mygo.RoleWindowMenu},
	})
}
