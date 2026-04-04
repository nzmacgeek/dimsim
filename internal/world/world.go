package world

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
)

const worldPath = "/var/lib/dimsim/world"

// Read returns the current set of explicitly installed package names.
func Read() (map[string]bool, error) {
	f, err := os.Open(worldPath)
	if os.IsNotExist(err) {
		return make(map[string]bool), nil
	}
	if err != nil {
		return nil, fmt.Errorf("open world file: %w", err)
	}
	defer f.Close()

	world := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			world[line] = true
		}
	}
	return world, scanner.Err()
}

// Add adds package names to the world file.
func Add(names ...string) error {
	world, err := Read()
	if err != nil {
		return err
	}
	for _, n := range names {
		world[n] = true
	}
	return write(world)
}

// Remove removes package names from the world file.
func Remove(names ...string) error {
	world, err := Read()
	if err != nil {
		return err
	}
	for _, n := range names {
		delete(world, n)
	}
	return write(world)
}

// Contains returns true if the package is in the world file.
func Contains(name string) (bool, error) {
	world, err := Read()
	if err != nil {
		return false, err
	}
	return world[name], nil
}

func write(world map[string]bool) error {
	names := make([]string, 0, len(world))
	for n := range world {
		names = append(names, n)
	}
	sort.Strings(names)

	f, err := os.OpenFile(worldPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("write world file: %w", err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, n := range names {
		fmt.Fprintln(w, n)
	}
	return w.Flush()
}
