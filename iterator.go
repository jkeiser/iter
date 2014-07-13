package xmlchan

import "errors"

var FINISHED = errors.New("FINISHED")

type Iterator interface {
	Next() (interface{}, error)
	// TODO should we add Close() and clean up if we exit iteration early? Or is
	// garbage collection good enough?
}

func ToSlice(iter Iterator) (list []interface{}, err error) {
	err = Each(iter, func(item interface{}) {
		list = append(list, item)
	})
	if err != nil {
		return nil, err
	}
	return list, err
}

type EachFunc func(interface{})

func Each(iter Iterator, eachFunc EachFunc) error {
	item, err := iter.Next()
	for err == nil {
		eachFunc(item)
		item, err = iter.Next()
	}
	return err
}

type EachWithErrorFunc func(interface{}) error

func EachWithError(iter Iterator, eachFunc EachWithErrorFunc) error {
	item, err := iter.Next()
	for err == nil {
		err = eachFunc(item)
		if err == nil {
			item, err = iter.Next()
		}
	}
	if err != FINISHED {
		return err
	}
	return nil
}

type MapFunc func(interface{}) interface{}

func Map(iter Iterator, mapFunc MapFunc) Iterator {
	return IteratorFunc(func() (interface{}, error) {
		item, err := iter.Next()
		if err != nil {
			return nil, err
		}
		return mapFunc(item), nil
	})
}

type SelectFunc func(interface{}) bool

func Select(iter Iterator, selectFunc SelectFunc) Iterator {
	return IteratorFunc(func() (interface{}, error) {
		for {
			item, err := iter.Next()
			if err != nil {
				return nil, err
			}
			if selectFunc(item) {
				return item, nil
			}
		}
	})
}

func Concat(iterators ...Iterator) Iterator {
	i := 0
	return IteratorFunc(func() (interface{}, error) {
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
	})
}

// Used in GoSafe()
type ChannelItem struct {
	Value interface{}
	Error error
}

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
func Go(iter Iterator) (channel <-chan ChannelItem, done chan<- bool) {
	mainChannel := make(chan ChannelItem)
	doneChannel := make(chan bool)
	go iterateToChannel(iter, mainChannel, doneChannel)
	return mainChannel, doneChannel
}

// for value := range iter.GoSimple(iterator) {
//   ...
// }
//
// With this method, two undesirable things can happen:
// - if the iteration stops early due to an error, you will not find out about that
//   (the channel will just get closed)
// - if callers panic or exit early without retrieving all values from the channel,
//   the goroutine will be
//
// That said, if you can make guarantees about no panics, this method makes
// calling code easier to read ...
func GoSimple(iter Iterator) (channel <-chan interface{}) {
	mainChannel := make(chan interface{})
	go iterateToChannelSimple(iter, mainChannel)
	return mainChannel
}

func iterateToChannel(iter Iterator, channel chan<- ChannelItem, done <-chan bool) {
	defer close(channel)
	err := EachWithError(iter, func(result interface{}) error {
		select {
		case channel <- ChannelItem{Value: result}:
			return nil
		case _, _ = <-done:
			// If we are told we're done early, we finish quietly.
			return FINISHED
		}
	})
	if err != nil {
		channel <- ChannelItem{Error: err}
	}
}

func iterateToChannelSimple(iter Iterator, channel chan<- interface{}) {
	defer close(channel)
	Each(iter, func(item interface{}) {
		channel <- item
	})
}

type IteratorFunc func() (interface{}, error)

func (iter IteratorFunc) Next() (interface{}, error) {
	return iter()
}
