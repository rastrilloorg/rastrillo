// Command genvectors is a second command, so finding Routes must not
// trip over more than one main package.
package main

import "fmt"

func main() { fmt.Println("vectors") }
