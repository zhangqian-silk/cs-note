# Function

## receiver

在函数的声明前，可以加上一个特定的接收者，限定该函数仅能有该接收者的实例调用，实现类似面向对象的效果。其中接收者可以使用值类型或指针类型。

```go
type dog struct { }

func (d dog)move() { }

func (d *dog)run() { }
```

在方法调用时，值类型与指针类型差异不大：

- 值类型的实例，可以同时调用值类型和指针类型接收者的方法
- 指针类型的实例，可以同时调用值类型和指针类型接收者的方法

在实际使用中，一般会更倾向于使用指针类型的接收者，更符合面向对象的特征，使得在每一次调用时，方法内部与调用者，是同一个实例。

但是在实现接口时，会存在一些差异：

- 以值类型实现接口，则该类型和对应指针类型，都会被认为实现了该接口
- 以指针类型实现接口，则仅有指针类型被认识实现了该接口

```go
type animal interface {
    run()
}

func test() {
    var anAnimal animal

    anAnimal = dog{} // compile error: cannot use dog{} (value of type dog) as animal value in assignment: dog does not implement animal (method run has pointer receiver)
}
```
