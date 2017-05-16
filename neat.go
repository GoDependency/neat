package neat

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"sync"
	"text/tabwriter"
	"time"
)

// Config consists of all hyperparameter settings for NEAT. It can be imported
// from a JSON file.
type Config struct {
	// neural network settings
	NumInputs  int `json:"numInputs"`  // number of inputs (including bias)
	NumOutputs int `json:"numOutputs"` // number of outputs

	// evolution settings
	NumGenerations  int     `json:"numGenerations"`  // number of generations
	PopulationSize  int     `json:"populationSize"`  // size of population
	InitFitness     float64 `json:"initFitness"`     // initial fitness score
	MinimizeFitness bool    `json:"minimizeFitness"` // true if minimizing fitness
	SurvivalRate    float64 `json:"survivalRate"`    // survival rate

	// mutation rates settings
	RatePerturb float64 `json:"ratePerturb"` // mutation by perturbing weights
	RateAddNode float64 `json:"rateAddNode"` // mutation by adding a node
	RateAddConn float64 `json:"rateAddConn"` // mutation by adding a connection

	// compatibility distance coefficient settings
	DistanceThreshold float64 `json:"distanceThreshold"` // distance threshold
	CoeffUnmatching   float64 `json:"coeffUnmatching"`   // unmatching genes
	CoeffMatching     float64 `json:"coeffMatching"`     // matching genes
}

// NewConfig creates a new instance of Config, given the name of a JSON file
// that consists of the hyperparameter settings.
func NewConfigJSON(filename string) (*Config, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	config := &Config{}
	decoder := json.NewDecoder(f)
	if err = decoder.Decode(&config); err != nil {
		return nil, err
	}
	return config, nil
}

// Summarize prints the summarized configuration on terminal.
func (c *Config) Summarize() {
	w := tabwriter.NewWriter(os.Stdout, 40, 1, 1, ' ', tabwriter.TabIndent)
	fmt.Fprintf(w, "==================================================\n")
	fmt.Fprintf(w, "Summary of NEAT hyperparameter configuration\t\n")
	fmt.Fprintf(w, "==================================================\n")
	fmt.Fprintf(w, "--------------------------------------------------\n")
	fmt.Fprintf(w, "Neural network settings\t\n")
	fmt.Fprintf(w, "--------------------------------------------------\n")
	fmt.Fprintf(w, "+ Number of inputs\t%d\t\n", c.NumInputs)
	fmt.Fprintf(w, "+ Number of outputs \t%d\t\n", c.NumOutputs)
	fmt.Fprintf(w, "--------------------------------------------------\n")
	fmt.Fprintf(w, "Evolution settings\t\n")
	fmt.Fprintf(w, "--------------------------------------------------\n")
	fmt.Fprintf(w, "+ Number of generations\t%d\t\n", c.NumGenerations)
	fmt.Fprintf(w, "+ Population size\t%d\t\n", c.PopulationSize)
	fmt.Fprintf(w, "+ Initial fitness score\t%.3f\t\n", c.InitFitness)
	fmt.Fprintf(w, "+ Fitness is being minimized\t%t\t\n", c.MinimizeFitness)
	fmt.Fprintf(w, "+ Rate of survival each generation\t%.3f\t\n", c.SurvivalRate)
	fmt.Fprintf(w, "--------------------------------------------------\n")
	fmt.Fprintf(w, "Mutation settings\t\n")
	fmt.Fprintf(w, "--------------------------------------------------\n")
	fmt.Fprintf(w, "+ Rate of perturbation of weights\t%.3f\t\n", c.RatePerturb)
	fmt.Fprintf(w, "+ Rate of adding a node\t%.3f\t\n", c.RateAddNode)
	fmt.Fprintf(w, "+ Rate of adding a connection\t%.3f\t\n", c.RateAddConn)
	fmt.Fprintf(w, "--------------------------------------------------\n")
	fmt.Fprintf(w, "Compatibility distance settings\t\n")
	fmt.Fprintf(w, "--------------------------------------------------\n")
	fmt.Fprintf(w, "+ Distance threshold\t%.3f\t\n", c.DistanceThreshold)
	fmt.Fprintf(w, "+ Unmatching connection genes\t%.3f\t\n", c.CoeffUnmatching)
	fmt.Fprintf(w, "+ Matching connection genes\t%.3f\t\n", c.CoeffMatching)
	fmt.Fprintf(w, "--------------------------------------------------\n")
	w.Flush()
}

// NEAT is the implementation of NeuroEvolution of Augmenting Topology (NEAT).
type NEAT struct {
	sync.Mutex

	Config     *Config        // configuration
	Population []*Genome      // population of genome
	Species    []*Species     // subpopulations of genomes grouped by species
	Evaluation EvaluationFunc // evaluation function
	Best       *Genome        // best performing genome

	nextGenomeID  int // genome ID that is assigned to a newly created genome
	nextSpeciesID int // species ID that is assigned to a newly created species
}

// New creates a new instance of NEAT with provided argument configuration and
// an evaluation function.
func New(config *Config, evaluation EvaluationFunc) *NEAT {
	nextGenomeID := 0
	nextSpeciesID := 0

	population := make([]*Genome, config.PopulationSize)
	for i := 0; i < config.PopulationSize; i++ {
		population[i] = NewGenome(nextGenomeID, config.NumInputs, config.NumOutputs)
		nextGenomeID++
	}

	// initialize the first species with a randomly selected genome
	s := NewSpecies(nextSpeciesID, population[rand.Intn(len(population))])
	species := []*Species{s}
	nextSpeciesID++

	return &NEAT{
		Config:        config,
		Population:    population,
		Species:       species,
		Evaluation:    evaluation,
		Best:          population[rand.Intn(len(population))],
		nextGenomeID:  nextGenomeID,
		nextSpeciesID: nextSpeciesID,
	}
}

// evaluateSequential evaluates all genomes in the population in sequence.
func (n *NEAT) evaluateSequential() {
	for _, genome := range n.Population {
		genome.Evaluate(n.Evaluation)
	}
}

// evaluateParallel evaluates all genomes in the population in parallel.
func (n *NEAT) evaluateParallel() {
	runtime.GOMAXPROCS(n.Config.PopulationSize)

	var wg sync.WaitGroup
	wg.Add(n.Config.PopulationSize)

	for _, genome := range n.Population {
		go func(g *Genome, efn EvaluationFunc) {
			defer wg.Done()
			genome.Evaluate(n.Evaluation)
		}(genome, n.Evaluation)

		time.Sleep(time.Millisecond)
	}

	wg.Wait()
}

// inheritSequential performs crossover and mutation within all species in
// sequence.
func (n *NEAT) inheritSequential() {
	nextGeneration := make([]*Genome, 0, n.Config.PopulationSize)

	comparisonFunc := func(g []*Genome) func(i, j int) bool {
		comparison := func(i, j int) bool {
			return g[i].Fitness < g[j].Fitness
		}
		if !n.Config.MinimizeFitness {
			comparison = func(i, j int) bool {
				return g[i].Fitness > g[j].Fitness
			}
		}
		return comparison
	}

	for _, s := range n.Species {
		// genomes in this species can inherit to the next generation, if two or
		// more genomes survive in this species, and there is room for more
		// children, i.e., at least one genome must be eliminated.
		numSurvived := int(math.Ceil(float64(len(s.Members)) *
			n.Config.SurvivalRate))
		numEliminated := len(s.Members) - numSurvived

		if numSurvived > 2 && numEliminated > 0 {
			sort.Slice(s.Members, comparisonFunc(s.Members))
			s.Members = s.Members[:numSurvived]

			nextGeneration = append(nextGeneration, s.Members...)
			for i := 0; i < numEliminated; i++ {
				perm := rand.Perm(numSurvived)
				parent0 := s.Members[perm[0]]
				parent1 := s.Members[perm[1]]

				child := Crossover(n.nextGenomeID, parent0, parent1)
				n.nextGenomeID++

				nextGeneration = append(nextGeneration, child)
			}
		}
	}

	// update the population with the new generation
	n.Population = nextGeneration
}

// inheritParallel performs crossover and mutation within all species in
// parallel.
func (n *NEAT) inheritParallel() {
	runtime.GOMAXPROCS(len(n.Species))

	var wg sync.WaitGroup
	wg.Add(len(n.Species))

	nextGeneration := struct {
		sync.Mutex
		population []*Genome // children genome for the next generation
	}{population: make([]*Genome, 0, n.Config.PopulationSize)}

	comparisonFunc := func(g []*Genome) func(i, j int) bool {
		comparison := func(i, j int) bool {
			return g[i].Fitness < g[j].Fitness
		}
		if !n.Config.MinimizeFitness {
			comparison = func(i, j int) bool {
				return g[i].Fitness > g[j].Fitness
			}
		}
		return comparison
	}

	for _, species := range n.Species {
		go func(s *Species) {
			defer wg.Done()

			// genomes in this species can inherit to the next generation, if two or
			// more genomes survive in this species, and there is room for more
			// children, i.e., at least one genome must be eliminated.
			numSurvived := int(math.Ceil(float64(len(s.Members)) *
				n.Config.SurvivalRate))
			numEliminated := len(s.Members) - numSurvived

			if numSurvived > 2 && numEliminated > 0 {
				sort.Slice(s.Members, comparisonFunc(s.Members))
				s.Members = s.Members[:numSurvived]

				for i := 0; i < numEliminated; i++ {
					perm := rand.Perm(numSurvived)
					parent0 := s.Members[perm[0]]
					parent1 := s.Members[perm[1]]

					n.Lock()
					child := Crossover(n.nextGenomeID, parent0, parent1)
					n.nextGenomeID++
					n.Unlock()

					nextGeneration.Lock()
					nextGeneration.population = append(nextGeneration.population, child)
					nextGeneration.Unlock()
				}
			}
		}(species)
	}

	wg.Wait()

	// update the population with the new generation
	n.Population = nextGeneration.population
}

// Run executes evolution.
func (n *NEAT) Run(verbose bool) {
	if verbose {
		n.Config.Summarize()
	}

	for i := 0; i < n.Config.NumGenerations; i++ {
		n.evaluateSequential()

		for _, genome := range n.Population {
			registered := false
			for i := 0; i < len(n.Species) && !registered; i++ {
				dist := Compatibility(n.Species[i].Representative, genome,
					n.Config.CoeffUnmatching, n.Config.CoeffMatching)

				if dist < n.Config.DistanceThreshold {
					n.Species[i].Register(genome, n.Config.MinimizeFitness)
					registered = true
				}
			}

			if !registered {
				n.Species = append(n.Species, NewSpecies(n.nextSpeciesID, genome))
				n.nextSpeciesID++
			}
		}

		n.inheritSequential()

		// update the best genome
		for _, genome := range n.Population {
			if genome.Fitness < n.Best.Fitness {
				if n.Config.MinimizeFitness {
					n.Best = genome
				}
			}
		}

		fmt.Println("Generation", i, "Number of species:", len(n.Species), "Best score:", n.Best.Fitness)
	}
}

// RunParallel executes evolution with parallel processing.
func (n *NEAT) RunParallel(summarize bool) {
	if summarize {
		n.Config.Summarize()
	}
	for i := 0; i < n.Config.NumGenerations; i++ {
		n.evaluateParallel()

		for _, genome := range n.Population {
			registered := false
			for i := 0; i < len(n.Species) && !registered; i++ {
				dist := Compatibility(n.Species[i].Representative, genome,
					n.Config.CoeffUnmatching, n.Config.CoeffMatching)

				if dist < n.Config.DistanceThreshold {
					n.Species[i].Register(genome, n.Config.MinimizeFitness)
					registered = true
				}
			}

			if !registered {
				n.Species = append(n.Species, NewSpecies(n.nextSpeciesID, genome))
				n.nextSpeciesID++
			}
		}

		n.inheritParallel()

		// update the best genome
		for _, genome := range n.Population {
			if genome.Fitness < n.Best.Fitness {
				if n.Config.MinimizeFitness {
					n.Best = genome
				}
			}
		}
	}
}
