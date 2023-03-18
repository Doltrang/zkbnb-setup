package phase1

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"runtime"
	"sync"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
)

const batchSize = 1048576 // 2^20

// Returns powers of b starting from a as [a, ba, ..., abⁿ⁻¹ ]
func powers(a, b *fr.Element, n int) []fr.Element {
	result := make([]fr.Element, n)
	result[0].Set(a)
	for i := 1; i < n; i++ {
		result[i].Mul(&result[i-1], b)
	}
	return result
}

// Multiply each element by b
func batchMul(a []fr.Element, b *fr.Element) {
	parallelize(len(a), func(start, end int) {
		for i := start; i < end; i++ {
			a[i].Mul(&a[i], b)
		}
	})
}

func scaleG1(dec *bn254.Decoder, enc *bn254.Encoder, N int, tau, multiplicand *fr.Element) (*bn254.G1Affine, error) {
	// Allocate batch with smallest of (N, batchSize)
	var initialSize = int(math.Min(float64(N), float64(batchSize)))
	buff := make([]bn254.G1Affine, initialSize)
	var firstPoint bn254.G1Affine
	var startPower fr.Element
	var scalars []fr.Element
	startPower.SetOne()

	remaining := N
	for remaining > 0 {
		// Read batch
		readCount := int(math.Min(float64(remaining), float64(batchSize)))
		fmt.Println("Iterations ", int(remaining/readCount))
		for i := 0; i < readCount; i++ {
			if err := dec.Decode(&buff[i]); err != nil {
				return nil, err
			}
		}

		// Compute powers for the current batch
		scalars = powers(&startPower, tau, readCount)

		// Update startPower for next batch
		startPower.Mul(&scalars[readCount-1], tau)

		// If there is α or β, then mul it with powers of τ
		if multiplicand != nil {
			batchMul(scalars, multiplicand)
		}

		// Process the batch
		parallelize(readCount, func(start, end int) {
			for i := start; i < end; i++ {
				var tmpBi big.Int
				scalars[i].BigInt(&tmpBi)
				buff[i].ScalarMultiplication(&buff[i], &tmpBi)
			}
		})

		// Write the batch
		for i := 0; i < readCount; i++ {
			if err := enc.Encode(&buff[i]); err != nil {
				return nil, err
			}
		}

		// Should be initialized in first batch only
		if firstPoint.X.IsZero() {
			if multiplicand == nil {
				// Set firstPoint to the second point  = [τ]
				firstPoint.Set(&buff[1])
			} else {
				// Set firstPoint to the first point  = [α] or [β]
				firstPoint.Set(&buff[0])
			}
		}

		// Update remaining
		remaining -= readCount
	}
	return &firstPoint, nil
}

func scaleG2(dec *bn254.Decoder, enc *bn254.Encoder, N int, tau *fr.Element) (*bn254.G2Affine, error) {
	// Allocate batch with smallest of (N, batchSize)
	var initialSize = int(math.Min(float64(N), float64(batchSize)))
	buff := make([]bn254.G2Affine, initialSize)
	var firstPoint bn254.G2Affine
	var startPower fr.Element
	var scalars []fr.Element
	startPower.SetOne()

	remaining := N
	for remaining > 0 {
		// Read batch
		readCount := int(math.Min(float64(remaining), float64(batchSize)))
		fmt.Println("Iterations ", int(remaining/readCount))
		for i := 0; i < readCount; i++ {
			if err := dec.Decode(&buff[i]); err != nil {
				return nil, err
			}
		}

		// Compute powers for the current batch
		scalars = powers(&startPower, tau, readCount)

		// Update startPower for next batch
		startPower.Mul(&scalars[readCount-1], tau)

		// Process the batch
		parallelize(readCount, func(start, end int) {
			for i := start; i < end; i++ {
				var tmpBi big.Int
				scalars[i].BigInt(&tmpBi)
				buff[i].ScalarMultiplication(&buff[i], &tmpBi)
			}
		})

		// Write the batch
		for i := 0; i < readCount; i++ {
			if err := enc.Encode(&buff[i]); err != nil {
				return nil, err
			}
		}

		// Should be initialized in first batch only
		if firstPoint.X.IsZero() {

			firstPoint.Set(&buff[1])

		}

		// Update remaining
		remaining -= readCount
	}
	return &firstPoint, nil
}

func randomize(r []fr.Element) {
	parallelize(len(r), func(start, end int) {
		for i := start; i < end; i++ {
			r[i].SetRandom()
		}
	})
}

func linearCombinationG1(dec *bn254.Decoder, N int) (bn254.G1Affine, bn254.G1Affine, error) {
	// Allocate batch with smallest of (N, batchSize)
	var initialSize = int(math.Min(float64(N), float64(batchSize)))
	buff := make([]bn254.G1Affine, initialSize)
	r := make([]fr.Element, initialSize)
	var L1, L2, tmpL1, tmpL2 bn254.G1Affine

	remaining := N
	for remaining > 0 {
		// Read batch
		readCount := int(math.Min(float64(remaining), float64(batchSize)))
		fmt.Println("Iterations ", int(remaining/readCount))
		for i := 0; i < readCount; i++ {
			if err := dec.Decode(&buff[i]); err != nil {
				return L1, L2, err
			}
		}

		// Generate randomness
		randomize(r)

		// Process the batch
		tmpL1.MultiExp(buff[:readCount-1], r, ecc.MultiExpConfig{})
		tmpL2.MultiExp(buff[1:readCount], r, ecc.MultiExpConfig{})
		L1.Add(&L1, &tmpL1)
		L2.Add(&L2, &tmpL2)

		// Update remaining
		remaining -= readCount
	}
	return L1, L2, nil
}

func linearCombinationG2(dec *bn254.Decoder, N int) (bn254.G2Affine, bn254.G2Affine, error) {
	// Allocate batch with smallest of (N, batchSize)
	var initialSize = int(math.Min(float64(N), float64(batchSize)))
	buff := make([]bn254.G2Affine, initialSize)
	r := make([]fr.Element, initialSize)
	var L1, L2, tmpL1, tmpL2 bn254.G2Affine

	remaining := N
	for remaining > 0 {
		// Read batch
		readCount := int(math.Min(float64(remaining), float64(batchSize)))
		fmt.Println("Iterations ", int(remaining/readCount))
		for i := 0; i < readCount; i++ {
			if err := dec.Decode(&buff[i]); err != nil {
				return L1, L2, err
			}
		}

		// Generate randomness
		randomize(r)

		// Process the batch
		tmpL1.MultiExp(buff[:readCount-1], r, ecc.MultiExpConfig{})
		tmpL2.MultiExp(buff[1:readCount], r, ecc.MultiExpConfig{})
		L1.Add(&L1, &tmpL1)
		L2.Add(&L2, &tmpL2)

		// Update remaining
		remaining -= readCount
	}
	return L1, L2, nil
}

func verifyContribution(current, prev Contribution) error {
	// Compute SP for τ, α, β
	tauSP := genSP(current.PublicKeys.Tau.S, current.PublicKeys.Tau.SX, prev.Hash[:], 1)
	alphaSP := genSP(current.PublicKeys.Alpha.S, current.PublicKeys.Alpha.SX, prev.Hash[:], 2)
	betaSP := genSP(current.PublicKeys.Beta.S, current.PublicKeys.Beta.SX, prev.Hash[:], 3)

	// Check for knowledge of toxic parameters
	if !sameRatio(current.PublicKeys.Tau.S, current.PublicKeys.Tau.SX, current.PublicKeys.Tau.SPX, tauSP) {
		return errors.New("couldn't verify knowledge of Tau")
	}
	if !sameRatio(current.PublicKeys.Alpha.S, current.PublicKeys.Alpha.SX, current.PublicKeys.Alpha.SPX, alphaSP) {
		return errors.New("couldn't verify knowledge of Alpha")
	}
	if !sameRatio(current.PublicKeys.Beta.S, current.PublicKeys.Beta.SX, current.PublicKeys.Beta.SPX, betaSP) {
		return errors.New("couldn't verify knowledge of Beta")
	}

	// Check for valid updates using previous parameters
	if !sameRatio(current.G1.Tau, prev.G1.Tau, tauSP, current.PublicKeys.Tau.SPX) {
		return errors.New("couldn't verify that TauG1 is based on previous contribution")
	}
	if !sameRatio(current.G1.Alpha, prev.G1.Alpha, alphaSP, current.PublicKeys.Alpha.SPX) {
		return errors.New("couldn't verify that AlphaTauG1 is based on previous contribution")
	}
	if !sameRatio(current.G1.Beta, prev.G1.Beta, betaSP, current.PublicKeys.Beta.SPX) {
		return errors.New("couldn't verify that BetaTauG1 is based on previous contribution")
	}
	if !sameRatio(current.PublicKeys.Tau.S, current.PublicKeys.Tau.SX, current.G2.Tau, prev.G2.Tau) {
		return errors.New("couldn't verify that TauG2 is based on previous contribution")
	}
	if !sameRatio(current.PublicKeys.Beta.S, current.PublicKeys.Beta.SX, current.G2.Beta, prev.G2.Beta) {
		return errors.New("couldn't verify that BetaG2 is based on previous contribution")
	}

	// Check hash of the contribution
	h := computeHash(&current)
	if !bytes.Equal(current.Hash, h) {
		return errors.New("couldn't verify hash of contribution")
	}

	return nil
}

// Check e(a₁, a₂) = e(b₁, b₂)
func sameRatio(a1, b1 bn254.G1Affine, a2, b2 bn254.G2Affine) bool {
	var na2 bn254.G2Affine
	na2.Neg(&a2)
	res, err := bn254.PairingCheck(
		[]bn254.G1Affine{a1, b1},
		[]bn254.G2Affine{na2, b2})
	if err != nil {
		panic(err)
	}
	return res
}

// Parallelize process in parallel the work function
func parallelize(nbIterations int, work func(int, int), maxCpus ...int) {

	nbTasks := runtime.NumCPU()
	if len(maxCpus) == 1 {
		nbTasks = maxCpus[0]
	}
	nbIterationsPerCpus := nbIterations / nbTasks

	// more CPUs than tasks: a CPU will work on exactly one iteration
	if nbIterationsPerCpus < 1 {
		nbIterationsPerCpus = 1
		nbTasks = nbIterations
	}

	var wg sync.WaitGroup

	extraTasks := nbIterations - (nbTasks * nbIterationsPerCpus)
	extraTasksOffset := 0

	for i := 0; i < nbTasks; i++ {
		wg.Add(1)
		_start := i*nbIterationsPerCpus + extraTasksOffset
		_end := _start + nbIterationsPerCpus
		if extraTasks > 0 {
			_end++
			extraTasks--
			extraTasksOffset++
		}
		go func() {
			work(_start, _end)
			wg.Done()
		}()
	}

	wg.Wait()
}
