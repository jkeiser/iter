package iter

import "errors"
import "log"

var FINISHED = errors.New("FINISHED")

type Iterator struct {
	// Get the next value (or error).
	// nil, iter.FINISHED indicates iteration is complete.
	Next func() (interface{}, error)
	// Close out resources in case iteration terminates early. Callers are *not*
	// required to call this if iteration completes cleanly (but they may).
	Close func()
}

func (iterator Iterator) Map(mapper func(interface{}) interface{}) Iterator {
	return Iterator{
		Next: func() (interface{}, error) {
			item, err := iterator.Next()
			if err != nil {
				return nil, err
			}
			return mapper(item), nil
		},
		Close: func() { iterator.Close() },
	}
}

func (iterator Iterator) Select(selector func(interface{}) bool) Iterator {
	return Iterator{
		Next: func() (interface{}, error) {
			for {
				item, err := iterator.Next()
				if err != nil {
					return nil, err
				}
				if selector(item) {
					return item, nil
				}
			}
		},
		Close: func() { iterator.Close() },
	}
}

func Concat(iterators ...Iterator) Iterator {
	i := 0
	return Iterator{
		Next: func() (interface{}, error) {
			if i >= len(iterators) {
				return nil, FINISHED
			}
			item, err := iterators[i].Next()
			if err == FINISHED {
				i++
				return nil, err
			} else if err != nil {
				i = len(iterators)
				return nil, err
			}
			return item, nil
		},
		Close: func() {
			// Close out remaining iterators
			for ; i < len(iterators); i++ {
				iterators[i].Close()
			}
		},
	}
}

func (iterator Iterator) Each(processor func(interface{})) error {
	defer iterator.Close()
	item, err := iterator.Next()
	for err == nil {
		processor(item)
		item, err = iterator.Next()
	}
	return err
}

func (iterator Iterator) EachWithError(processor func(interface{}) error) error {
	defer iterator.Close()
	item, err := iterator.Next()
	for err == nil {
		err = processor(item)
		if err == nil {
			item, err = iterator.Next()
		}
	}
	return err
}

// Used in Go()
type ChannelItem struct {
	Value interface{}
	Error error
}

// Runs the iterator in a goroutine
// Allows you to process errors in the iteration, as well as clean up the goroutine.
//
// channel, done := iter.Go(iterator)
// defer close(done) // if early return or panic happens, this will clean up the goroutine
// for item := range channel {
//   if item.Error != nil {
//     // Iteration failed; handle the error and exit the loop
//     ...
//   }
//   value := item.Value
//   ...
// }
func (iterator Iterator) Go() (items <-chan ChannelItem, done chan<- bool) {
	itemsChannel := make(chan ChannelItem)
	doneChannel := make(chan bool)
	go iterator.IterateToChannel(itemsChannel, doneChannel)
	return itemsChannel, doneChannel
}

// Iterates all items and sends them to the given channel.  Runs on the current
// goroutine (call go iterator.IterateToChannel to set it up on a new goroutine).
func (iterator Iterator) IterateToChannel(items chan<- ChannelItem, done <-chan bool) {
	defer close(items)
	err := iterator.EachWithError(func(result interface{}) error {
		select {
		case items <- ChannelItem{Value: result}:
			return nil
		case _, _ = <-done:
			// If we are told we're done early, we finish quietly.
			return FINISHED
		}
	})
	if err != nil {
		items <- ChannelItem{Error: err}
	}
}

func EachFromChannel(items <-chan ChannelItem, done chan<- bool, processor func(interface{}) error) error {
	defer close(done) // if early return or panic happens, this will clean up the goroutine
	for item := range items {
		if item.Error != nil {
			return item.Error
		}
		err := processor(item.Value)
		if err != nil {
			return err
		}
	}
	return nil
}

// Perform the iteration in the background concurrently with the Each() statement.
// Useful when the iterator or iteratee will be doing blocking work.
// iterator.BackgroundEach(100, func(item interface{}) { ... })
func (iterator Iterator) BackgroundEach(bufferSize int, processor func(interface{}) error) error {
	itemsChannel := make(chan ChannelItem, bufferSize)
	doneChannel := make(chan bool)
	go iterator.IterateToChannel(itemsChannel, doneChannel)
	return EachFromChannel(itemsChannel, doneChannel, processor)
}

// for value := range iter.GoSimple(iterator) {
//   ...
// }
//
// With this method, two undesirable things can happen:
// - if the iteration stops early due to an error, you will not be able to handle
//   it (the goroutine will log and panic, and the program will exit).
// - if callers panic or exit early without retrieving all values from the channel,
//   the goroutine is blocked forever and leaks.
//
// The Go() routine allows you to handle both of these issues, at a small cost to
// caller complexity.
//
// That said, if you can make guarantees about no panics, this method makes
// calling code easier to read ...
func (iterator Iterator) GoSimple() (values <-chan interface{}) {
	mainChannel := make(chan interface{})
	go iterator.IterateToChannelSimple(mainChannel)
	return mainChannel
}

func (iterator Iterator) IterateToChannelSimple(values chan<- interface{}) {
	defer close(values)
	err := iterator.Each(func(item interface{}) {
		values <- item
	})
	if err != nil {
		log.Fatalf("Iterator returned an error in GoSimple: %v", err)
	}
}

func (iterator Iterator) ToSlice() (list []interface{}, err error) {
	err = iterator.Each(func(item interface{}) {
		list = append(list, item)
	})
	return list, err
}
