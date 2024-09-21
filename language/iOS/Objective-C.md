# OC

## 数据类型

### NSInteger与int

NSInteger在64位内核中为long，32位内核中为int

### BOOL与bool

BOOL的值为YES和NO

### CGFloat与float

CGFloat在64位内核中为double，32位内核中为float

### id(identifier)与instancetype

id是一种泛型，用来引用==任何类型==的对象（实际上是指向对象结构体的指针）

instancetype表示某个方法返回的未知类型的Objective-C对象

1. instancetype与id都可以作为方法的返回类型
2. ==instancetype可以返回和方法所在类相同类型的对象==，id只能返回未知类型
3. instancetype只能作为返回值，而id还可以作为参数

### nil与NULL

nil表示指向==OC对象==的指针为空，NULL表示指向==基本数据类型==的指针为空

### NSString

是Cocoa中用于处理字符串的类，标志为双引号前面的@符号

```objective-c
@"Hello, Objective-C!"
```

## 函数

### NSLog()

```objective-c
NSLog(@"Hello, Objective-C!");
```

NSLog()接受一个字符串作为第一个参数，该字符串可以包含格式说明符并接受与格式说明符相匹配的其他参数（类比于printf()）

## 消息传递

在Objective-C，类别与消息的关系比较松散，调用方法视为对对象发送消息，所有方法都被视为对消息的回应。==所有消息处理直到运行时（runtime）才会动态决定==，并交由类别自行决定如何处理收到的消息。也就是说，一个类别不保证一定会回应收到的消息，如果类别收到了一个无法处理的消息，程序只会抛出异常，不会出错或崩溃。

```objective-c
[obj method: argument];
```

## 类

### 类的定义（interface）

定义了类的名称、数据成员和方法，以关键字@interface作为开始，@end作为结束。方法前，（+）表示类方法，（-）表示实例方法。

```objective-c
@interface MyObject : NSObject {
    int memberVar1; // 实体变量
    id  memberVar2;
}

+(return_type) class_method; // 类方法

-(return_type) instance_method1; // 实例方法
-(return_type) instance_method2: (int) p1;
-(return_type) instance_method3: (int) p1 andPar: (int) p2;
@end //MyObject
```

中缀语法：==冒号是方法名称的一部分==，用来提示后面会出现参数。参数的类型在圆括号中指定，且紧跟冒号。

优点：可以在方法名内对参数进行说明，更加清晰明了，优秀的命名会更加优雅（如果按照 : 进行换行对齐，看起来还会莫名的好看）。尤其是在大型项目时，对于方法名而言，“清晰”比“简洁”更加重要。而且，有时候我们可能会只对外部提供少数几个公开的方法名，每一个方法对应一个特别复杂的操作，这个时候可能会需要传入较多参数，中缀语法的优点也就越明显。

缺点：批评好像大多数都是在说其啰嗦、冗长，不过配合上xcode的自动补全，还是可以接受的

```objective-c
[UICommand commandWithTitle:@"从URL添加"
                      image:nil
                     action:@selector(addImageFromURL)
               propertyList:nil];
```

### 类的实现（implementation）

包含公开方法的实现，以及私有变量和方法的定义。以关键字@implementation作为区块起头，@end结尾。

```objective-c
@implementation MyObject {
  int memberVar3; //私有实体变量
}

+(return_type) class_method {
    .... //method implementation
}
-(return_type) instance_method1 {
     ....
}
-(return_type) instance_method2: (int) p1 {
    ....
}
-(return_type) instance_method3: (int) p1 andPar: (int) p2 {
    ....
}
@end //MyObject
```

@implementation中可以定义@interface中没有声明过的方法，可以看做仅能在当前类实现中使用的私有方法。==但是Objective-C中不存在真正私有的方法==，也不能把某个方法标识为私有方法。

### 类的创建

Objective-C创建对象需通过alloc以及init两个消息，alloc的作用是分配内存，init则是初始化对象

```objective-c
MyObject * my = [[MyObject alloc] init];
```

在Objective-C 2.0里，若创建对象不需要参数，则可直接使用new

```objective-c
MyObject * my = [MyObject new];
```

在Objective-C中，类最终都会转化为一个结构体存在，==类的实例对象其实是指向该结构体的指针==

也可以自定义初始化方法，命名格式为init+With+参数

```objective-c
@interface MyObject : NSObject {
    int var1; // 实体变量
    float var2;
}
- (instancetype) initWithVar1: (int) newVar1 andVar2: (float) newVar2;
@end //MyObject
    
@implementation MyObject
- (instancetype) initWithVar1: (int) newVar1 andVar2: (float) newVar2 {
    self = [super init];
    if (self) {
        var1 = newVar1;
        var2 = newVar2;
    }
    return self;
}
@end //MyObject
    
MyObject * my = [[MyObject alloc] initWithVar1: newVar1 andVar2: newVar2];
```

可以使用工厂方法来创建新对象，工厂方法是一个==类方法==，命名要以类名开头

```objective-c
@interface MyObject : NSObject {
    int var1; // 实体变量
    float var2;
}
+ (MyObject*) myObject; //无参工厂方法
+ (MyObject*) myObjectWithVar1: (int) newVar1 andVar2: (float) newVar2; //有参工厂方法
@end //MyObject
    
@implementation MyObject
+ (MyObject*) myObject {
    return [Myobject new]
}
+ (MyObject*) myObjectWithVar1: (int) newVar1 andVar2: (float) newVar2 {
    return [[MyObjevt alloc] initWithVar1: newVar1 andVar2: newVar2]
}
@end //MyObject
    
MyObject * my = [MyObject myObjectWithVar1: newVar1 andVar2: newVar2];
```

### 类的继承

子类定义时不再声明继承的变量或方法，==如果没有声明实例变量也不需要花括号==，==同时要注意有没有分号==

```objective-c
@interface MyObject : NSObject {
    int memberVar1; // 实体变量
    id  memberVar2;
}

+(return_type) class_method; // 类方法

-(return_type) instance_method1; // 实例方法
-(return_type) instance_method2: (int) p1;
-(return_type) instance_method3: (int) p1 andPar: (int) p2;
@end //MyObject
```

```objective-c
@interface MyChild : MyObject //这里注意没有分号
@end //MyChild
```

子类在实现时，可以重写父类方法，父类方法中，如果没有实现任何功能，仍要需要定义

```objective-c
@implementation MyObject {
  int memberVar3; //私有实体变量
}

+(return_type) class_method {
    .... //method implementation
}
-(return_type) instance_method1 {
     // instance_method1即使不实现任何功能，也要定义
}
-(return_type) instance_method2: (int) p1 {
    ....
}
-(return_type) instance_method3: (int) p1 andPar: (int) p2 {
    ....
}
@end //MyObject
```

```objective-c
@implementation MyChild
- (return_type) instance_method1{
    .... //重写父类方法
}
@end // MyChild
```

子类在重写方法的同时，还==可以通过super调用父类的实现==

```objective-c
@implementation MyChild
- (return_type) instance_method1{
    .... //重写父类方法
    [super instance_method1]; //调用父类方法
}
@end // MyChild
```

### 类的复合

通过包含作为实例变量的对象指针来实现

```objective-c
@interface MyObject : NSObject{
 Object1 *pObject1;
    Object2 *pObject2;
}
@end // MyObject
```

### 存取方法

用来读取或改变某个对象属性的方法，`Setter`方法以前缀set加上所要更改的属性名称命名，`Getter`方法以返回的属性名称命名==（要注意`Getter`方法不能以get作为前缀）==

```objective-c
@interface MyObject : NSObject{
    int var1;
    float var2;
}
- (void) setVar1: (int) newVar1; //setter
- (int) var1; //getter
- (void) setVar2: (float) newVar2; //setter
- (float) var2; //getter
@end //MyObject
    
@implementation MyObject
- (void) setVar1: (int) newVar1 {
    var1 = newVar1;
} //setVar1
- (int) var1 {
    return var1;
}
- (void) setVar2: (float) newVar2 {
    var2 = newVar2;
} //setVar2
- (float) var2 {
    return var2;
}
@end //MyObject
```

### 属性

使用属性`@property type myProperty`，编译器会自动为该属性生成名为`_myPropertyd`的成员变量，同时会自动为声明为属性的成员变量添加`Setter`和`Getter`方法

```objective-c
@property (attribute1 [, attribute2,...]) type name;
```

属性的修饰符可选项为

1. 原子性，针对于存取方法：默认`atomic`, `nonatomic`
2. 存取特性：默认`readwrite`, `readonly`
3. 内存管理特性：默认`strong`, `weak`, `copy`, `assign`, `retain`
4. 重命名存取方法：`setter = xxx`, `getter = xxx`
5. Nullability:
   1. `nullable`：对象可以为空
   2. `nonnull`：对象不能为空
   3. `null_unspecified`：未指定（默认）
   4. `null_resettable`：==`Setter`可以传入nil，但是`Getter`的返回值不为空==

`assign`为简单赋值，不影响计数及内存管理，可以修饰对象和基本数据类型，但是修饰对象可能会产生野指针问题

`weak`为弱引用，只能修饰对象，但是当对象释放后，指针会置为`nil`，不会产生野指针问题

属性允许使用点表达式，如果点表达式出现在等号（=）左边，将调用该变量的`Setter`方法，如果点表达式出现在等号（=）右边，将调用该变量的`Getter`方法

如果想要属性和实例的名称不一样，可以在实现中进行设置

```objective-c
@property type name;
@synthesize name = newName;
@synthesize name = _name //默认
```

如果不想要编译器来创建实例变量、`Setter`和`Getter`方法，可以在实现中指明

```objective-c
@property type name;
@dynamic name;
```

### 类别

类别是一种为现有的类添加新方法的方法，而不需要知道源代码。

类别代码通常放在独立的文件里，以“类名称+类别名称”来命名

```objective-c
// myObject+myCategory.h
#import myObject.h
@interface myObject (myCategory)
- (void) newMethod;
@end //myCategory
    
// myObject+myCategory.m
#import myObject+myCategory.h
@implementation myObject (myCategory)
- (void) newMethod{
    ....
} //newMethod
@end //myCategory
```

类别不能向类中添加新的实例变量，如果与原有的方法重名时，会==覆盖原方法==

### 类拓展

在类的实现文件中进行声明。可以添加新的属性与实例变量，也可以修改接口文件中声明的权限。外部只能引用头文件中定义的属性、成员、方法等。

```objective-c
@interface UIImageView (BDWebImage)

@end
    
@implementation UIImageView (BDWebImage)

@end
```

### 选择器

选择器`selector`是一个方法名称，以`@selector()`编译指令来指定选择器，以Objective-C运行时使用的特殊方式进行编码，可以通过respondsToSelector:  方法进行快速查询

```objective-c
if ([myObject respondsToSelector: @selector(print)]){
    [myObject print];
}
```

### 协议

协议是包含了方法和属性的有名称列表，要求显式地采用。如果某个类要采用某协议，想要在类的声明中列出协议名称，在实现中实现该协议==要求实现==的所有方法

```objective-c
@protocol myProtocol <myParentProtocol>
@required //必须实现的
- (void) method1;
@optional //可选的
- (void) method2;
@end //myProtocol
    
@interface myObject : NSObject <myProtocol>
@end //myObject

@implementation myObject
- (void) method1{
    ....
} //method1
- (void) method2{
    ....
} //method2
@end
```

可以在使用数据类型时，为实例变量和方法参数指定协议名称，如在id类型后指定协议名称，则编译器会检查对象是否遵守该协议

```objective-c
- (void) myMethod: (id <myProtocol>) anObject;
```

同时在使用时，推荐结合选择器一起使用，来检查是否实现了该方法

### 委托（delegate）

某个对象指定另一个对象处理某些特定任务的设计模式。==委托要遵守协议==，所以对象会知道委托可以完成的任务，同时，只要遵守所需的协议，就可以设置任意对象为委托。

```objective-c
//myPtotocol
@protocol myProtocol <NSObject>
@optional
- (void)doSomeOptionalWork;
@required
- (void)doSomeRequiredWork;
@end

//myObject
@interface myObject : NSObject
@property (weak) id <myProtocol> delegate;
- (void)doWork;
@end //myObject

@implementation myObject
@synthesize delegate;
- (void)doWork{
    [delegate doSomeRequiredWork];
    if([delegate respondsToSelector:@selector(doSomeOptionalWork)]){
        [delegate doSomeOptionalWork];
    }
} //dowork
@end //myObject
  
//myDelegate
@interface myDelegate : NSObject <myProtocol>
@end //myDelegate

@implementation myDelegate
- (void) doSomeRequiredWork{
    ....
} //doSomeRequiredWork
- (void) doSomeOptionalWork{
    ....
} //doSomeOptionalWork
@end //myDelegate
```

## Cocoa类

### NSObject类

#### descripting

`NSLog`打印%@时，实际打印的是该对象调用`description`方法的返回值，所有`NSObejct`的子类都可以重写该方法

```objective-c
@interface MyObject : NSObject
@end // MyObject

@interface MyObject
- (NSString *) description{
    return @"...."; //重写description方法，指定打印内容
}
@end // MyObject
```

#### init

用于实例的初始化，实例变量会初始化为默认值

#### respondsToSelector

询问对象以确定能否响应某个特定的消息

```objective-c
[myObject responsToSelector: @selector(method)]
```

#### conformsToProtocol

询问对象是否遵守了某协议

```objective-c
[myObject conformsToProtocol: @protocol(myProtocol)]
```

#### isKindOfClass

询问对象是否是myObject类或是其子类

```objective-c
[obj isKindOfClass: [myObject class]]
```

#### isMemberOfClass

询问对象是否是myObject类

```objective-c
[obj isMemberOfClass: [myObject class]]
```

### NSString

- 使用NSString类的stringWithFormat: 方法格式化创建NSString

   ```objective-c
   + (id) stringWithFormat: (NSString *) format, ...;
   NSString *myString = [NSString stringWithFormat: @“Hello, i am %d years old”, 21];
   ```

- length方法可以返回字符串中的字符个数

   ```objective-c
   - (NSUInteger) length;
   NSUInteger length = [myString length];
   ```

- isEqualToString: 方法判断字符串是否相等

   ```objective-c
   - (BOOL) isEqualToString: (NSString*) aString;
   
   if ([myString1 isEqualToString: myString2]){
       NSLog(@"They are the same!");
   }
   ```

- compare: 方法比较字符串大小（按字母表中排序位置进行逐字符的比较，==大写字母要大于小写字母==）

   ```objective-c
   enum {
       NSOrderedAscending = -1,
       NSOrderedSame,
       NSOrderedDescending
   };
   typedef NSInteger NSComparisonResult;
   - (NSComparisonResult) compare: (NSString*) aString;
   
   if ([myString1 compare: myString2] == 0){
       NSLog(@"myString1 == mySring2");
   } else if ([myString1 compare: myString2] < 0){
       NSLog(@"myString1 < myString2");
   } else if ([myString1 compare: myString2] > 0){
       NSLog(@"myString1 > myString2");
   }
   ```

- compare: options: 方法提供更多的可选的比较逻辑，options参数可以用or运算符（｜）来添加标记选项

  - NSCaseInsensitiveSearch：不区分大小写字符

  - NSLiteralSearch：进行完全比较，区分大小写字符

  - NSNumericSearch：数字按数字大小比较，非数字正常比较，即把所有相邻的数字当作一个单位来比较，还是比较坑，建议参考下文

     [字符串比较中NSNumericSearch选项的工作原理](https://juejin.cn/post/6844903784800321544)

   ```objective-c
   - (NSComparisonResult) compare: (NSString*) aString
       options: (NSStringCompareOptions) mask;
   ```

- hasPrefix: 方法可以检查字符串是否以另一个字符串开头

   ```objective-c
   - (BOOL) hasPrefix: (NSString*) aString;
   ```

- hasSuffix: 方法可以检查字符串是否以另一个字符串结尾

   ```objective-c
   - (BOOL) hasSuffix: (NSString*) aString;
   ```

- rangeOfString: 方法可以找到指定字符在字符串中的范围，返回NSRange类

   ```objective-c
   - (NSRange) rangeOfString: (NSString*) aString; 
   ```

- 通过componentsSeparatedByString: 方法可以把字符串切分为数组

   ```objective-c
   NSString *myString = @"one:two:three:four:five";
   NSArray *myArray = [myString]
   ```

- 截取字符串

   ```objective-c
   myString1 = [myString substringFromIndex: 2];
   myString2 = [myString substringTOIndex: 6];
   myString3 = [myString substringWithRange: NSMakeRange(2,6)];
   ```

- 读取操作

    ```objective-c
    - (BOOL)writeToFile:(NSString *)path 
             atomically:(BOOL)useAuxiliaryFile 
               encoding:(NSStringEncoding)enc 
                  error:(NSError * _Nullable *)error;
    - (BOOL)writeToURL:(NSURL *)url 
            atomically:(BOOL)useAuxiliaryFile 
              encoding:(NSStringEncoding)enc 
                 error:(NSError * _Nullable *)error;
    
    + (instancetype)stringWithContentsOfFile:(NSString *)path 
                                    encoding:(NSStringEncoding)enc 
                                       error:(NSError * _Nullable *)error;
    + (instancetype)stringWithContentsOfURL:(NSURL *)url 
                                   encoding:(NSStringEncoding)enc 
                                      error:(NSError * _Nullable *)error;
    ```

- `NSString`是不可变的，而`NSString`的子类`NSMutableString`是可变的

    ```objective-c
    + (id) stringWithCapacity: (NSUInteger) capacity;
    - (void) setString: (NSString*) aString;
    - (void) appendString: (NSString*) aString;
    - (void) appendFormat: (NSString*) format,...;
    - (void) insertString: (NSString*) aString 
                  atIndex: (NSUInteger) i;
    - (void) deleteCharactersInRange: (NSRange) aRange;
    - (void) replaceCharacterInrange: (NSRange) aRange
                          withString: (NSString*) aString;
    ```

### NSArray

用来存储对象的有序列表。可以通过类方法arrayWithObjects: 创建，并以nil结尾表示结束，也可以通过字面量的格式来创建。

```objective-c
NSArray *array1 = [NSArray arrayWithObjects: @"one", 
                 @"two", @"three", nil];
NSArray *array2 = @[@"one", @"two", @"three"];
```

通过count方法获取元素个数

```objective-c
- (NSUInteger) count;
```

通过objectAtIndex: 方法或字面量访问特定索引处元素

```objective-c
- (id) objectAtIndex: (NSUInteger) index;
id myObject = array[1];
```

通过`indexOfObject:`获取对象位置

```objective-c
- (NSUInteger) indexOfObject: (id) anObject;
```

通过componentsJoinedByString: 可以合并NSArray中的元素并创建字符串

```objective-c
myString = [myArray componentsJoinedByString: @":"]
```

通过`enumerateObjectsUsingBlock:`遍历数组元素并执行代码块

```objective-c
NSArray *array = @[@0, @1, @2, @3, @4, @5];
__block NSInteger count = 0;
[array enumerateObjectsUsingBlock:^(NSNumber *number, NSUInteger idx, BooL *stop) {
 if([number compare:@2] == NSOrderedAscending) {
        count++;
   }
}] ;
```

```objective-c
// 读取操作
- (BOOL)writeToURL:(NSURL *)url error:(NSError * _Nullable *)error;
+ (NSArray<ObjectType> *)arrayWithContentsOfURL:(NSURL *)url 
                                          error:(NSError * _Nullable *)error;
```

NSArray是不可变的，而NSArray的子类NSMutableArray是可变的，可以考虑使用`mutableCopy`来创建

```objective-c
+ (id) arrayWithCapacity: (NSUInteger) numItems;
- (void) addObject: (id) anObject;
- (void) insertObject: (id) anObject atIndex: (NSUInteger) index;
- (void) removeObjectAtIndex: (NSUInteger) index;
- (void) removeObject: (id) anObject;
- (void) replaceObjectatIndex: (NSUInteger) index withObject: (id) anObject;
```

### NSEumerator

用来表示集合中迭代出的对象，需要通过objectEnumerator向数组请求枚举器，也可以通过reverseObjectEnumerator进行反向枚举

```objective-c
- (NSEnumerator *) objectEnumerator;
NSEnumerator *enumerator = [array objectEnumerator];
NSEnumerator *enumerator = [array reverseObjectEnumerator];
```

获得枚举器后，可以通过nextObject向枚举器请求下一个对象

```objective-c
- (id) nextObject;
NSEnumerator *enumerator = [array objectEnumerator];
while (id myObject = [enumerator nextObject]){
    NSLog(@"I found %@", myObject);
}
```

也通过 for - in 进行快速枚举

```objective-c
for (NSString *myString in myArray){
    NSLog(@"%@", myString);
}
```

### NSDictionary

字典是关键字及其定义的集合

```objective-c
// 创建字典
+ (id) dictionaryWithObjectsAndKeys: (id) firstObject,...;
NSDictionary *myDictionary1 = [NSDictionaty dictionaryWithObjectsAndKeys:
                              myObject1, @"first", myObject2, @"second", nil];
NSDictionary *myDictionary2 = @{@"first" : myObject1, @"second" : myObject2}; 

//访问对象
- (id) objectForKey: (id) aKey;
myObject myObject1 = [myDictionary objectForKey: @"first"];
myObject myObject2 = myDictionary[@"seconde"];

// 获取 key 或 value
- (NSArray*) allKeys;
- (NSArray*) allValues;

// 读取操作
- (BOOL)writeToURL:(NSURL *)url error:(NSError * _Nullable *)error;
+ (NSDictionary<NSString *,ObjectType> *)
    dictionaryWithContentsOfURL:(NSURL *)url error:(NSError * _Nullable *)error;
```

NSDictionary是不可变的，而NSDictionary的子类NSMutableDictionary是可变的

```objective-c
+ (id) dictionaryWithCapacity: (NSUInteger) numItems;
+ (id) dictionaryWithDictionary: (NSDictionary*) aDictionary;

// value 为 nil 时，调用 removeObject，value 不为 nil 时，调用 setObject
- (void)setValue:(id)value forKey:(NSString *)key;
// anObject 不能为 nil
- (void) setObject: (id) anObject forKey: (id) aKey;
- (void) removeObjectForKey: (id) aKey;
```

### NSSet

### NSNumber

NSNumber可以用来封装基本数据类型，但是==不能进行数值计算==

```objective-c
+ (NSNumber*) numberWithChar: (char) value;
+ (NSNumber*) numberWithInt: (int) value;
+ (NSNumber*) numberWithFloat: (float) value;
+ (NSNumber*) numberWithBool: (BOOL) value;
```

也可以通过字面量的方法来创建

```objective-c
NSNumber *myNumber;
myNumber = @'X'; //字符型
myNumber = @12345; //整型
myNumber = @12345ul; //无符号长整型
myNumber = @123.45f; //单浮点型
myNumber = @123.45; //双浮点型
myNumber = @YES; //布尔值
```

可以通过实例方法来重新获得基本类型

```objective-c
- (char) charValue;
- (int) intValue;
- (float) floatValue;
- (BOOL) boolValue;
- (NSString*) stringValue;
```

### NSValue

NSValue主要用来封装结构体（对象），传递的参数是所要封装的目标的地址

```objective-c
// 封装
+ (NSValue*) valueWithBytes: (const void*) value 
       objectType: (const char*) type;
// 提取数据
- (void) getValue: (void*) buffer;
```

可以通过一些方法将常用的struct型数据封装为NSValue

```objective-c
+ (NSValue *) valueWithPoint: (NSPoint)aPoint;
+ (NSValue *) valueWithSize: (NSSize)size;
+ (NSValue *) valueWithRect: (NSRect)rect;
- (NSPoint) pointValue;
- (NSSize) sizeValue;
- (NSRect) rectValue;
```

### NSNull

用来代替nil，表示空值。

```objective-c
+ (NSNull*) null;
```

### NSURL

> [NSURLSession最全攻略](https://www.jianshu.com/p/ac79db251cbf)

创建URL格式，可以通过多种方式构造

```objective-c
// 创建文件URL，不需要自己添加file://
+ (NSURL *)fileURLWithPath:(NSString *)path isDirectory:(BOOL)isDir;
// 根据字符串创建URL，常用于网络
+ (nullable instancetype)URLWithString:(NSString *)URLString;
```

### NSData

二进制数据，可以用来==包装各种其他类型的数据==，如字符串、文本、音频、图像等

```objective-c
// 读取操作，path的上级文件必须存在
- (BOOL)writeToFile:(NSString *)path atomically:(BOOL)useAuxiliaryFile;
- (BOOL)writeToURL:(NSURL *)url atomically:(BOOL)atomically;
+ (instancetype)dataWithContentsOfFile:(NSString *)path;
+ (instancetype)dataWithContentsOfURL:(NSURL *)url;
```

## Copy

浅拷贝：对于非容器类，拷贝对象的地址，对于容器类，重新分配一块内存给容器，即容器地址不一样，但是容器装的内容的地址一样

深拷贝：对于非容器类，重新分配一块内容，完全拷贝目标对象，对于非容器类，完全拷贝目标容器内对象，容器的地址和内容的地址都不一样

`retain/strong`：浅拷贝，对目标的引用计数加一

`copy`：对可变对象为深拷贝，引用计数不变；对不可变对象为浅拷贝，引用计数加一，需要实现`NSCopying`协议

`mutableCopy`：深拷贝，引用计数不变，需要实现`NSMutableCopying`协议

## 代码块

> [iOS Block原理探究以及循环引用的问题](https://www.jianshu.com/p/9ff40ea1cee5)

代码块对象是对于C语言中函数的拓展，又是也被称被闭包。==在声明代码块变量和代码块实现的开头位置，要使用幂操作符==，但是调用是不需要。同时需要注意==代码块结束时，要有分号==

```objective-c
<returnType> (^blockname) (list of arguments) = 
    ^(arguments) {....};
blockname();
```

使用`typedef`关键字可以声明代码块变量

```objective-c
typedef <returnType> (^blockname) (list of arguments);
blockname myBlock =  ^(arguments) {....};
```

也可以直接定义或执行而不指定名称

```objective-c
^{ .... };
^{ .... }();
```

代码块可以访问==与它相同的有效范围内声明的变量==，但是代码块会在定义时==复制并保存它们的状态==，不会因为后续的修改而改变

```objective-c
int value = 1;
int (^func) (int number) = 
    ^(int number){
    return number + value;
};
```

想要在代码块内修改的变量，需要声明为`_block`，而如果是`static`或是`global`变量，block捕获的是它们的指针，不需要声明为`_block`也可以修改

```objective-c
_block int value;
void (^func) (int a, int b) = 
    ^(int a, int b){
    c = a * b;
};
```

block内部只使用了全局变量或没有使用任务外部的局部变量时，会被分配在内存中的全局区，类型为`_NSConcreteGlobalBlock`，其他情况下会分配在栈区，类型为`_NSConcreteStackBlock`，当手动或自动执行`copy`时，会存储在堆区，类型为`_NSConcreteMallocBlock`

ARC下，以下几种情况，系统会自动执行`copy`

- 当 block 作为函数返回值返回时；
- 当 block 被赋值给`__strong`修饰的 id 类型的对象或 block 对象时；
- 当 block 作为参数被传入方法名带有 usingBlock 的 Cocoa Framework 方法或 GCD 的 API 时(比如使用NSArray的enumerateObjectsUsingBlock和GCD的dispatch_async方法时，其block不需要我们手动执行copy操作)
   注：系统方法内部对block进行了copy操作

因为在ARC下，对象默认是用`__strong`修饰的，所以大部分情况下编译器都会将 block从栈自动复制到堆上，除了以下情况

- block 作为方法或函数的参数传递时，编译器不会自动调用 copy 方法；
- block 作为临时变量，没有赋值给其他block

## block关于self的使用

1. 直接在 block 里面使用关键词self
   1. 如果 block 被属性 retain，self 和 block 之间会有一个循环引用并且它们不会再被释放。
   2. 如果 block 被传送并且被其他的对象 copy 了，self 在每一个 copy 里面被 retain
2. 在 block 外定义一个__weak的 引用到 self，并且在 block 里面使用这个弱引用
   1. 不管 block 是否通过属性被 retain ，这里都不会发生循环引用。
   2. 如果 block 被传递或者 copy 了，在执行的时候，weakSelf 可能已经变成 nil。
3. 在 block 外定义一个__weak引用 self，并在 block 内部通过这个弱引用定义一个__strong的引用。
   1. 不管 block 是否通过属性被 retain ，这里也不会发生循环引用。
   2. 如果 block 被传递到其他对象并且被复制了，执行的时候，如果使用强引用，可以确保对象在 block 调用的完整周期里面被正确retain。

## 内存管理

### MRC

MRC关键字：

- 生成：`alloc`, `new`, `copy`
- 持有：`retain`
- 释放：`release`, `aoturelease`
- 废弃：`dealloc`
- 显示：`retainCount`

通过`alloc`, `new`, `copy`获得的对象，需要`release`, `aoturelease`该对象，通过其他方式获得的对象，不需要执行任何操作，如果选择`retain`，则需要负责`release`, `aoturelease`

线程在一个`AotuReleasePool`的上下文执行时，可以选择`aoturelease`，在一次事件循环结束后，`AotuReleasePool`里面的变量会自动释放

如果两个对象互相`retain`，则会造成循环引用，需要把其中一个`retain`改为`assign`

### ARC

系统自己来进行内存管理，在需要的地方去插入`retain`, `release`, `aoturelease`

ARC中，如果对于循环引用，需要使用`weak`，而不能使用`assign`

## 并发

### GCD

同步执行：==同步==添加任务到指定队列，在添加到任务执行结束前，会一直等待

```objective-c
dispatch_queue_t queue = dispatch_get_main_queue();
dispatch_sync(queue, ^{
        // 想执行的任务
});
```

异步执行：==异步==添加任务到指定队列，不会做等待，可以继续执行任务

```objective-c
dispatch_queue_t queue = dispatch_get_main_queue();
dispatch_async(queue, ^{
        // 想执行的任务
});
```

串行队列`Serial Queue`：每个串行队列按顺序执行任务，每个队列使用一个线程

```objective-c
dispatch_queue_t queue = dispatch_queue_create("MySerialDiapatchQueue", DISPATCH_QUEUE_SERIAL);

dispatch_async(queue, ^{ NSLog(@"thread1"); });
dispatch_async(queue, ^{ NSLog(@"thread2"); });
dispatch_async(queue, ^{ NSLog(@"thread3"); });
```

并行队列`Concurrent Queue`：每个队列同时执行一个或多个任务，但仍按照添加顺序执行

```objective-c
dispatch_queue_t queue = dispatch_queue_create("MyConcurrentDiapatchQueue", DISPATCH_QUEUE_CONCURRENT);

dispatch_async(queue, ^{ NSLog(@"thread1"); });
dispatch_async(queue, ^{ NSLog(@"thread2"); });
dispatch_async(queue, ^{ NSLog(@"thread3"); });
```

主队列：一种串行队列，执行应用程序的主线程任务

并发队列：全局并发队列，是四个并行队列，优先级分别为`High`, `Default`, `Low`, `Background`

```objective-c
dispatch_queue_t globalDiapatchQueueHigh = dispatch_get_global_queue(DISPATCH_QUEUE_PRIORITY_HIGH, 0);

dispatch_queue_t globalDiapatchQueueDefault = dispatch_get_global_queue(DISPATCH_QUEUE_PRIORITY_DEFAULT, 0);

dispatch_queue_t globalDiapatchQueueLow = dispatch_get_global_queue(DISPATCH_QUEUE_PRIORITY_LOW, 0);

dispatch_queue_t globalDiapatchQueueBackground = dispatch_get_global_queue(DISPATCH_QUEUE_PRIORITY_BACKGROUND, 0);
```

### NSOperation

通过`NSInvocationOperation`创建

```objective-c
- (void)useInvocationOperation {
    NSInvocationOperation *op = [[NSInvocationOperation alloc] initWithTarget:self selector:@selector(task) object:nil];
    [op start];
}
```

通过`NSBlockOperation`创建

```objective-c
- (void)useBlockOperation {
    NSBlockOperation *op = [NSBlockOperation blockOperationWithBlock: ^{ .... } ];
    [op addExecutionBlock: ^{ .... }];
    [op start];
}
```

创建队列

```objective-c
// 获取主队列
NSOperationQueue *queue = [NSOperationQueue mainQueue];

// 创建一个自定义队列
NSOperationQueue *queue = [[NSOperationQueue alloc] init];
```

添加队列

```objective-c
[queue addOperation: op1];
```

设置最大并发数，数值为1时为串行队列

```objective-c
queue.maxConcurrentOperationCount = number;
```

设置依赖，控制线程间同步关系

```objective-c
[op2 addDependency: op1]; // op1->op2
```

设置优先级

```objective-c
// 优先级的取值
typedef NS_ENUM(NSInteger, NSOperationQueuePriority) {
    NSOperationQueuePriorityVeryLow = -8L,
    NSOperationQueuePriorityLow = -4L,
    NSOperationQueuePriorityNormal = 0,
    NSOperationQueuePriorityHigh = 4,
    NSOperationQueuePriorityVeryHigh = 8
};

@interface NSOperation : NSObject
@property NSOperationQueuePriority queuePriority;
@end
```

## 文件

### NSFileManager

主要是对文件进行操作以及对于文件信息的获取

```objective-c
// 创建文件管理对象
@property (class, readonly, strong) NSFileManager *defaultManager 

// 判断某个路径的状态，isDirectory是一个指针，表示该路径是否是目录
- (BOOL)fileExistsAtPath:(NSString *)path;
- (BOOL)fileExistsAtPath:(NSString *)path isDirectory:(nullable BOOL *)isDirectory;
- (BOOL)isReadableFileAtPath:(NSString *)path;
- (BOOL)isWritableFileAtPath:(NSString *)path;
- (BOOL)isExecutableFileAtPath:(NSString *)path;
- (BOOL)isDeletableFileAtPath:(NSString *)path;

// 创建一个目录
-(BOOL)createDirectoryAtPath:(NSString *)path 
 withIntermediateDirectories:(BOOL)createIntermediates 
       attributes:(nullable NSDictionary<NSString *, id> *)attributes 
                       error:(NSError **)error;
// 创建一个文件,可顺便写入data
-(BOOL)createFileAtPath:(NSString *)path 
         contents:(nullable NSData *)data 
             attributes:(nullable NSDictionary<NSString *, id> *)attr;

// 对于文件的操作，其中移动操作可用来重命名
-(BOOL)moveItemAtPath:(NSString *)srcPath 
         toPath:(NSString *)dstPath 
                error:(NSError **)error;
- (BOOL)moveItemAtPath:(NSString *)srcPath 
       toPath:(NSString *)dstPath 
                 error:(NSError **)error;
-(BOOL)removeItemAtPath:(NSString *)path error:(NSError **)error;

// 获取当前文件夹下的文件/目录
-(nullable NSArray<NSString *> *)
      contentsOfDirectoryAtPath:(NSString *)path error:(NSError **)error;
// 获取文件信息(文件大小、修改时间、所有者等)
-(nullable NSDictionary<NSFileAttributeKey, id> *)
      attributesOfItemAtPath:(NSString *)path error:(NSError **)error;

```

```objective-c
// NSData类型的写入数据
-(BOOL)writeToFile:(NSString *)path 
     atomically:(BOOL)useAuxiliaryFile;

// SString、NSArray、NSDictionary的写入数据
-(BOOL)writeToFile:(NSString *)path 
     atomically:(BOOL)useAuxiliaryFile 
          encoding:(NSStringEncoding)enc 
             error:(NSError **)error;
```

### NSFileHandle

主要是对文件内容进行读取和写入操作

```objective-c
// 写的方式打开文件
+(nullable instancetype)fileHandleForWritingAtPath:(NSString *)path;
// 读的方式打开文件
+(nullable instancetype)fileHandleForReadingAtPath:(NSString *)path;
// 跳到文件末尾
-(unsigned long long)seekToEndOfFile;
// 跳到指定偏移位置
-(void)seekToFileOffset:(unsigned long long)offset;
// 将文件的长度设为offset字节
-(void)truncateFileAtOffset:(unsigned long long)offset;
// 从当前字节读取到文件到末尾数据
-(NSData *)readDataToEndOfFile;
// 从当前字节读取到指定长度数据
-(NSData *)readDataOfLength:(NSUInteger)length;
// 同步文件，通常用在写入数据后
-(void)synchronizeFile;
// 关闭文件
-(void)closeFile;
```

## 通信方式

### KVC

Key-Value Coding，通过属性名称和字符串间接访问属性，`keyPath`指的是多层级的属性，如`person.dog.name`

```objective-c
- (nullable id)valueForKey:(NSString *)key;                          //直接通过Key来取值
- (void)setValue:(nullable id)value forKey:(NSString *)key;          //通过Key来设值
- (nullable id)valueForKeyPath:(NSString *)keyPath;                  //通过KeyPath来取值
- (void)setValue:(nullable id)value forKeyPath:(NSString *)keyPath;  //通过KeyPath来设值
```

调用时，会按照如下规则运行，==一定要注意命名规范==

- 在调用`setValue: forKey:`的时，程序优先调用`setName:`方法，如果没有找到`setName:`方法 KVC会检查这个类的` + (BOOL)accessInstanceVariablesDirectly `类方法看是否返回YES（默认YES），返回YES则会继续查找该类有没有名为`_name`的成员变量，如果还是没有找到则会继续查找`_isName`成员变量，还是没有则依次查找`name`、`isName`。上述的成员变量都没找到则执行`setValue:forUndefinedKey:`抛出异常，如果不想程序崩溃应该重写该方法。假如这个类重写了`+ (BOOL)accessInstanceVariablesDirectly` 返回的是NO，则程序没有找到`setName:`方法之后，会直接执行`setValue:forUndefinedKey:`抛出异常。
- 在调用`valueForKey:`的时，会依次按照`getName`，`name`，`isName`的顺序进行调用。如果这3个方法没有找到，那么KVC会按照`countOfName`、`objectInNameAtIndex`来查找。如果查找到这两个方法就会返回一个数组。如果还没找到则调用`+ (BOOL)accessInstanceVariablesDirectly`看是否返回YES，返回YES则依次按照`_name`、`_isName`、`name`、`isName`顺序查找成员变量名，还是没找到就调用`valueForUndefinedKey:`；返回NO直接调用`valueForUndefinedKey:`

### KVO

Key-Value Obersver，对目标对象的某属性添加观察，当该属性==按照KVC发生改变==时，自动通知观察者，即触发观察者实现的KVO的接口方法

```objective-c
// 添加观察者
- (void)addObserver:(NSObject *)observer 
      forKeyPath:(NSString *)keyPath 
      options:(NSKeyValueObservingOptions)options 
         context:(nullable void *)context;
// 接收通知
- (void)observeValueForKeyPath:(nullable NSString *)keyPath 
          ofObject:(nullable id)object 
            change:(nullable NSDictionary<NSString*, id> *)change 
                 context:(nullable void *)context;
// 移除
- (void)removeObserver:(NSObject *)observer forKeyPath:(NSString *)keyPath;
```

当某个类的对象第一次被“观察”时，系统在运行期会创建一个派生类，==在派生类中重写`setter`方法==，在更新属性值的前后分别调用两个方法进行通知，并在`didChangeValueForKey:`中，调用上述提到的接受通知的方法

```objectivec
- (void)willChangeValueForKey:(NSString *)key;
- (void)didChangeValueForKey:(NSString *)key;
```
