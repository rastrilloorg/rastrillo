package groomer

type Booking struct{ Dog string }

func NewBooking(dog string) Booking {
	return Booking{Dog: dog}
}

func (booking Booking) Name() string {
	return booking.Dog
}
