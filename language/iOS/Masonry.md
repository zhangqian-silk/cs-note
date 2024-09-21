# Masonry

对系统api进行封装后的第三方自动布局框架，需要引入头文件“Masonry.h”

基础API

```objective-c
mas_makeConstraints()    添加约束
mas_remakeConstraints()  移除之前的约束，重新添加新的约束
mas_updateConstraints()  更新约束，写哪条更新哪条，其他约束不变

equalTo()       参数是对象类型，一般是视图对象或者mas_width这样的坐标系对象
mas_equalTo()   和上面功能相同，参数可以传递基础数据类型对象，可以理解为比上面的API更强大

width()         用来表示宽度，例如代表view的宽度
mas_width()     用来获取宽度的值。和上面的区别在于，一个代表某个坐标系对象，一个用来获取坐标系对象的值
    
不带有mas_前缀时，括号内必须为id类型，即@100，带有mas_前缀会自动把基础类型转为id类型
```

在引入头文件之前，可以用两个宏定义进行声明，就不需要区分`mas_`前缀了

```objective-c
// 定义这个常量，就可以不用在开发过程中使用"mas_"前缀。
#define MAS_SHORTHAND
// 定义这个常量，就可以让Masonry帮我们自动把基础数据类型的数据，自动装箱为对象类型。
#define MAS_SHORTHAND_GLOBALS
#import "Masonry.h"
```

更新约束和布局

```objective-c
// 调用此方法，如果有标记为需要重新布局的约束，则立即进行重新布局，内部会调用updateConstraints方法
- (void)updateConstraintsIfNeeded  
// 重写此方法，内部实现自定义布局过程
- (void)updateConstraints      
// 当前是否需要重新布局，内部会判断当前有没有被标记的约束    
- (BOOL)needsUpdateConstraints    
- (void)setNeedsUpdateConstraints  标记需要进行重新布局
    
- (void)setNeedsLayout  标记为需要重新布局
- (void)layoutIfNeeded  查看当前视图是否被标记需要重新布局，有则在内部调用layoutSubviews方法进行重新布局
- (void)layoutSubviews  重写当前方法，在内部完成重新布局操作
```

`Masonry`中，所有的约束属性类型都是`MASConstraint`，且方法都会返回`MASConstraint`，同时加上设置`delegate`为`MASConstraintMaker`，才具有链式语法

```objective-c
[self.yellowView mas_makeConstraints:^(MASConstraintMaker *make) {
    make.left.equalTo(self.view).with.offset(10);
    make.top.equalTo(self.view).with.offset(10);
    make.right.equalTo(self.view).with.offset(-10);
    make.bottom.equalTo(self.view).with.offset(-10);
    make.width.and.height.equalTo(100);
}];
```

# MASLayoutConstraint

继承自 `NSLayoutConstraint` ，用来表示布局约束，添加了属性`mas_key`

# MASViewAttribute

用来表示约束方程式(如下图)中`item`和`attribute`的组合

![view-formula](/Users/bytedance/Documents/iOS/Masonry/view-formula.png)

```objective-c
@interface MASViewAttribute : NSObject
@property (nonatomic, weak, readonly) MAS_VIEW *view;
@property (nonatomic, weak, readonly) id item;
@property (nonatomic, assign, readonly) NSLayoutAttribute layoutAttribute;
- (id)initWithView:(MAS_VIEW *)view 
   layoutAttribute:(NSLayoutAttribute)layoutAttribute;
- (id)initWithView:(MAS_VIEW *)view 
    		  item:(id)item 
   layoutAttribute:(NSLayoutAttribute)layoutAttribute;
```

# MASConstraint

视图约束的抽象类，为子类声明了一些共有方法，并实现了部分功能。

## 操作方法

操作方法按约束方程式，可分为下述几类：

- 属性操作方法（Attribute）
- 关系操作方法（Relationship）
- 倍数操作方法（Multiplier）
- 常量操作方法（Constant）
- 优先级操作方法

### 属性操作方法

根据 `NSLayoutAttribute` 枚举类型来创建约束属性项

```objective-c
- (MASConstraint *)left;
- (MASConstraint *)top;
- (MASConstraint *)right;
- (MASConstraint *)bottom;
- (MASConstraint *)leading;
- (MASConstraint *)trailing;
- (MASConstraint *)width;
- (MASConstraint *)height;
- (MASConstraint *)centerX;
- (MASConstraint *)centerY;
- (MASConstraint *)baseline;

- (MASConstraint *)firstBaseline;
- (MASConstraint *)lastBaseline;

- (MASConstraint *)leftMargin;
- (MASConstraint *)rightMargin;
- (MASConstraint *)topMargin;
- (MASConstraint *)bottomMargin;
- (MASConstraint *)leadingMargin;
- (MASConstraint *)trailingMargin;
- (MASConstraint *)centerXWithinMargins;
- (MASConstraint *)centerYWithinMargins;
```

方法内部由`addConstraintWithLayoutAttribute:` 来实现，==子类需要对方法内容具体实现==

### 关系操作方法

根据 `NSLayoutRelation` 枚举类型创建约束关系项

```objective-c
- (MASConstraint * (^)(id attr))equalTo;
- (MASConstraint * (^)(id attr))greaterThanOrEqualTo;
- (MASConstraint * (^)(id attr))lessThanOrEqualTo;
```

方法内部由`equalToWithRelation:`来实现，==子类需要对方法内容具体实现==

### 倍数操作方法

两个倍数操作方法都是抽象方法，须由子类具体实现。

```objective-c
- (MASConstraint * (^)(CGFloat multiplier))multipliedBy;
- (MASConstraint * (^)(CGFloat divider))dividedBy;
```

### 常量操作方法

常量操作方法内部各自调用对应的 `setter` 方法，而这些 `setter` 方法都是抽象方法，须由子类具体实现。

```objective-c
- (MASConstraint * (^)(MASEdgeInsets insets))insets;
- (MASConstraint * (^)(CGFloat inset))inset;
- (MASConstraint * (^)(CGSize offset))sizeOffset;
- (MASConstraint * (^)(CGPoint offset))centerOffset;
- (MASConstraint * (^)(CGFloat offset))offset;
- (MASConstraint * (^)(NSValue *value))valueOffset;
```

### 优先级操作方法

后三个优先级操作方法根据 `NSLayoutPriority` 枚举类型设置约束优先级，其内部都是通过调用第一个优先级操作方法实现的，该方法为抽象方法，须子类具体实现。

```
- (MASConstraint * (^)(MASLayoutPriority priority))priority;
- (MASConstraint * (^)())priorityLow;
- (MASConstraint * (^)())priorityMedium;
- (MASConstraint * (^)())priorityHigh;
```

## MASViewConstraint

是`MASConstraint`的子类，能够==完整地表示约束方程式==，存储了约束的优先级属性

```objective-c
// Public
@property (nonatomic, strong, readonly) MASViewAttribute *firstViewAttribute;
@property (nonatomic, strong, readonly) MASViewAttribute *secondViewAttribute;

// Private
@property (nonatomic, strong, readwrite) MASViewAttribute *secondViewAttribute;
@property (nonatomic, weak) MAS_VIEW *installedView;                // 约束被添加至的位置/视图
@property (nonatomic, weak) MASLayoutConstraint *layoutConstraint;  // 约束
@property (nonatomic, assign) NSLayoutRelation layoutRelation;      // 关系
@property (nonatomic, assign) MASLayoutPriority layoutPriority;     // 优先级
@property (nonatomic, assign) CGFloat layoutMultiplier;             // 倍数
@property (nonatomic, assign) CGFloat layoutConstant;               // 常量
@property (nonatomic, assign) BOOL hasLayoutRelation;
@property (nonatomic, strong) id mas_key;
@property (nonatomic, assign) BOOL useAnimator;
```

对于上述提到的抽象方法，提供了具体实现

属性操作方法将具体实现交由代理`MASConstraintMaker`完成

```objective-c
- (MASConstraint *)addConstraintWithLayoutAttribute:(NSLayoutAttribute)layoutAttribute {
    // 必须是没有设置过布局关系，即 hasLayoutRelation 为 NO
    NSAssert(!self.hasLayoutRelation, 
             @"Attributes should be chained before defining the constraint relation");
    return [self.delegate constraint:self addConstraintWithLayoutAttribute:layoutAttribute];
}
```

关系操作方法区分约束的类别是否是数组，然后分别进行处理

```objective-c
- (MASConstraint * (^)(id, NSLayoutRelation))equalToWithRelation {
    return ^id(id attribute, NSLayoutRelation relation) {
        if ([attribute isKindOfClass:NSArray.class]) {
            // 必须是没有设置过布局关系，即 hasLayoutRelation 为 NO
            NSAssert(!self.hasLayoutRelation, @"Redefinition of constraint relation");
            // 如果 attribute 是一组属性，则生成一组约束
            NSMutableArray *children = NSMutableArray.new;
            for (id attr in attribute) {
                MASViewConstraint *viewConstraint = [self copy];
                viewConstraint.layoutRelation = relation;
                viewConstraint.secondViewAttribute = attr;
                [children addObject:viewConstraint];
            }
            // 将一组约束转换成组合约束，并将代理所持有对应的约束进行替换
            MASCompositeConstraint *compositeConstraint = 
                		[[MASCompositeConstraint alloc] initWithChildren:children];
            compositeConstraint.delegate = self.delegate;
            [self.delegate constraint:self 
       shouldBeReplacedWithConstraint:compositeConstraint];
            return compositeConstraint;
        } else {
            NSAssert(!self.hasLayoutRelation 
                     || self.layoutRelation == relation 
                     && [attribute isKindOfClass:NSValue.class], 
                     @"Redefinition of constraint relation");
            // 如果 attribute 是单个属性，则设置约束的第二项
            self.layoutRelation = relation;
            self.secondViewAttribute = attribute;
            return self;
        }
    };
}
```

倍数操作方法对倍数属性`layoutMultiplier`进行赋值

```objective-c
- (MASConstraint * (^)(CGFloat))multipliedBy {
    return ^id(CGFloat multiplier) {
        NSAssert(!self.hasBeenInstalled,
                 @"Cannot modify constraint multiplier after it has been installed");
        self.layoutMultiplier = multiplier;
        return self;
    };
}

- (MASConstraint * (^)(CGFloat))dividedBy {
    return ^id(CGFloat divider) {
        NSAssert(!self.hasBeenInstalled,
                 @"Cannot modify constraint multiplier after it has been installed");
        self.layoutMultiplier = 1.0/divider;
        return self;
    };
}
```

常量操作方法针对属性的不同，分别进行实现，实现时还需要通过`switch-case`来检查约束属性

```objective-c
// 注意右和下，方法实现中带有负号
- (void)setInsets:(MASEdgeInsets)insets {
    NSLayoutAttribute layoutAttribute = self.firstViewAttribute.layoutAttribute;
    switch (layoutAttribute) {
        case NSLayoutAttributeLeft:
        case NSLayoutAttributeLeading:
            self.layoutConstant = insets.left;
            break;
        case NSLayoutAttributeTop:
            self.layoutConstant = insets.top;
            break;
        case NSLayoutAttributeBottom:
            self.layoutConstant = -insets.bottom;
            break;
        case NSLayoutAttributeRight:
        case NSLayoutAttributeTrailing:
            self.layoutConstant = -insets.right;
            break;
        default:
            break;
    }
}

// 上下左右设置为同一值
- (void)setInset:(CGFloat)inset {
    [self setInsets:(MASEdgeInsets){
        .top = inset, .left = inset, .bottom = inset, .right = inset
    }];
}

- (void)setOffset:(CGFloat)offset {
    self.layoutConstant = offset;
}

- (void)setSizeOffset:(CGSize)sizeOffset {
    NSLayoutAttribute layoutAttribute = self.firstViewAttribute.layoutAttribute;
    switch (layoutAttribute) {
        case NSLayoutAttributeWidth:
            self.layoutConstant = sizeOffset.width;
            break;
        case NSLayoutAttributeHeight:
            self.layoutConstant = sizeOffset.height;
            break;
        default:
            break;
    }
}

- (void)setCenterOffset:(CGPoint)centerOffset {
    NSLayoutAttribute layoutAttribute = self.firstViewAttribute.layoutAttribute;
    switch (layoutAttribute) {
        case NSLayoutAttributeCenterX:
            self.layoutConstant = centerOffset.x;
            break;
        case NSLayoutAttributeCenterY:
            self.layoutConstant = centerOffset.y;
            break;
        default:
            break;
    }
}
```

优先级操作方法直接设置优先级属性 `layoutPriority`

```objective-c
- (MASConstraint * (^)(MASLayoutPriority))priority {
    return ^id(MASLayoutPriority priority) {
        NSAssert(!self.hasBeenInstalled,
                 @"Cannot modify constraint priority after it has been installed");
        self.layoutPriority = priority;
        return self;
    };
}
```

## MASCompositeConstraint

也是`MASConstraint`的子类，用于表示一组约束，用`childConstraints`来持有

```objective-c
// Private
@property (nonatomic, strong) NSMutableArray *childConstraints;
```

同时，该类设置委托为`self`，并实现了委托方法，==但是实际实现代理方法的还是`MASConstrainMaker`==

属性操作方法同样调用`constraint:addConstraintWithLayoutAttribute:`方法，但是由自己实现

```objective-c
- (MASConstraint *)addConstraintWithLayoutAttribute:(NSLayoutAttribute)layoutAttribute {
    [self constraint:self addConstraintWithLayoutAttribute:layoutAttribute];
    return self;
}

// 新创建约束并添加至childConstraints中
- (MASConstraint *)constraint:(MASConstraint __unused *)constraint 
    addConstraintWithLayoutAttribute:(NSLayoutAttribute)layoutAttribute {
    id<MASConstraintDelegate> strongDelegate = self.delegate;
    MASConstraint *newConstraint = [strongDelegate constraint:self 
                             addConstraintWithLayoutAttribute:layoutAttribute];
    newConstraint.delegate = self;
    [self.childConstraints addObject:newConstraint];
    return newConstraint;
}
```

关系操作方法、倍数操作方法、常量操作方法、优先级操作方法与`MASViewConstraint`相同，对`childConstraints`中元素进行遍历操作

## install

`install`方法在`MASViewConstraint`中实现，`MASCompositeConstraint`中对元素进行遍历操作

`install`方法负责创建`MASLayoutConstraint`，并将该对象添加至对应的View上

```objective-c
- (void)install {
    if (self.hasBeenInstalled) {
        return;
    }
    
    if ([self supportsActiveProperty] && self.layoutConstraint) {
        self.layoutConstraint.active = YES;
        [self.firstViewAttribute.view.mas_installedConstraints addObject:self];
        return;
    }
    
    MAS_VIEW *firstLayoutItem = self.firstViewAttribute.item;
    NSLayoutAttribute firstLayoutAttribute = self.firstViewAttribute.layoutAttribute;
    MAS_VIEW *secondLayoutItem = self.secondViewAttribute.item;
    NSLayoutAttribute secondLayoutAttribute = self.secondViewAttribute.layoutAttribute;

    // alignment attributes must have a secondViewAttribute
    // therefore we assume that is refering to superview
    // eg make.left.equalTo(@10)
    if (!self.firstViewAttribute.isSizeAttribute && !self.secondViewAttribute) {
        secondLayoutItem = self.firstViewAttribute.view.superview;
        secondLayoutAttribute = firstLayoutAttribute;
    }
    
    // 创建约束
    MASLayoutConstraint *layoutConstraint
        = [MASLayoutConstraint constraintWithItem:firstLayoutItem
                                        attribute:firstLayoutAttribute
                                        relatedBy:self.layoutRelation
                                           toItem:secondLayoutItem
                                        attribute:secondLayoutAttribute
                                       multiplier:self.layoutMultiplier
                                         constant:self.layoutConstant];
    
    layoutConstraint.priority = self.layoutPriority;
    layoutConstraint.mas_key = self.mas_key;
    
    // 寻找添加约束的View
    if (self.secondViewAttribute.view) {
        // 如果是两个视图的相对约束，寻找两个视图的公共父视图
        MAS_VIEW *closestCommonSuperview = [self.firstViewAttribute.view 
                             mas_closestCommonSuperview:self.secondViewAttribute.view];
        NSAssert(closestCommonSuperview,
                 @"couldn't find a common superview for %@ and %@",
                 self.firstViewAttribute.view, self.secondViewAttribute.view);
        self.installedView = closestCommonSuperview;
    } else if (self.firstViewAttribute.isSizeAttribute) {
        // 如果是Size类型的约束，就添加在当前视图
        self.installedView = self.firstViewAttribute.view;
    } else {
        // 如果都不是，就添加在当前视图的父视图
        self.installedView = self.firstViewAttribute.view.superview;
    }
	
    // 添加视图
    MASLayoutConstraint *existingConstraint = nil;
    if (self.updateExisting) {
        existingConstraint = [self layoutConstraintSimilarTo:layoutConstraint];
    }
    if (existingConstraint) {
        // 更新约束
        existingConstraint.constant = layoutConstraint.constant;
        self.layoutConstraint = existingConstraint;
    } else {
        // 添加约束
        [self.installedView addConstraint:layoutConstraint];
        self.layoutConstraint = layoutConstraint;
        [firstLayoutItem.mas_installedConstraints addObject:self];
    }
}
```

# MASConstraintMaker

用于构建布局的目标视图及约束，并提供了一系列的`MASConstraint`属性，`getter`方法内会创建 `NSLayoutAttribute` 枚举类型的约束项

```objective-c
@property (nonatomic, weak) MAS_VIEW *view;
@property (nonatomic, strong) NSMutableArray *constraints;

@property (nonatomic, strong, readonly) MASConstraint *left;
@property (nonatomic, strong, readonly) MASConstraint *top;
@property (nonatomic, strong, readonly) MASConstraint *right;
@property (nonatomic, strong, readonly) MASConstraint *bottom;
@property (nonatomic, strong, readonly) MASConstraint *leading;
@property (nonatomic, strong, readonly) MASConstraint *trailing;
@property (nonatomic, strong, readonly) MASConstraint *width;
@property (nonatomic, strong, readonly) MASConstraint *height;
@property (nonatomic, strong, readonly) MASConstraint *centerX;
@property (nonatomic, strong, readonly) MASConstraint *centerY;
@property (nonatomic, strong, readonly) MASConstraint *baseline;

@property (nonatomic, strong, readonly) MASConstraint *firstBaseline;
@property (nonatomic, strong, readonly) MASConstraint *lastBaseline;

@property (nonatomic, strong, readonly) MASConstraint *leftMargin;
@property (nonatomic, strong, readonly) MASConstraint *rightMargin;
@property (nonatomic, strong, readonly) MASConstraint *topMargin;
@property (nonatomic, strong, readonly) MASConstraint *bottomMargin;
@property (nonatomic, strong, readonly) MASConstraint *leadingMargin;
@property (nonatomic, strong, readonly) MASConstraint *trailingMargin;
@property (nonatomic, strong, readonly) MASConstraint *centerXWithinMargins;
@property (nonatomic, strong, readonly) MASConstraint *centerYWithinMargins;

@property (nonatomic, strong, readonly) MASConstraint *(^attributes)(MASAttribute attrs);

@property (nonatomic, strong, readonly) MASConstraint *edges;
@property (nonatomic, strong, readonly) MASConstraint *size;
@property (nonatomic, strong, readonly) MASConstraint *center;
```

## MASConstraintDelegate方法

这里的重点，是==设置`delegate`为自己==，这样在链式调用时，第一次调用会返回`MASConstraint`类型，后面的调用则可以通过`delegate`正确的找到方法

```objective-c
// 在已有的约束中查找某个约束进行替换
- (void)constraint:(MASConstraint *)constraint 
    shouldBeReplacedWithConstraint:(MASConstraint *)replacementConstraint {
    NSUInteger index = [self.constraints indexOfObject:constraint];
    NSAssert(index != NSNotFound, @"Could not find constraint %@", constraint);
    [self.constraints replaceObjectAtIndex:index withObject:replacementConstraint];
}

- (MASConstraint *)constraint:(MASConstraint *)constraint 
    addConstraintWithLayoutAttribute:(NSLayoutAttribute)layoutAttribute {
    // 根据视图和属性创建viewAttribute
    MASViewAttribute *viewAttribute = 
        [[MASViewAttribute alloc] initWithView:self.view layoutAttribute:layoutAttribute];
    // 创建约束
    MASViewConstraint *newConstraint = 
        			[[MASViewConstraint alloc] initWithFirstViewAttribute:viewAttribute];
    if ([constraint isKindOfClass:MASViewConstraint.class]) {
        // 如果存在约束，则转化为一个组合约束，并替换原约束
        NSArray *children = @[constraint, newConstraint];
        MASCompositeConstraint *compositeConstraint = 
            		[[MASCompositeConstraint alloc] initWithChildren:children];
        // 设置delegate
        compositeConstraint.delegate = self;
        [self constraint:constraint shouldBeReplacedWithConstraint:compositeConstraint];
        return compositeConstraint;
    }
    if (!constraint) {
        // 设置delegate
        newConstraint.delegate = self;
        [self.constraints addObject:newConstraint];
    }
    return newConstraint;
}
```

此外，还实现了`addConstraintWithAttributes:`方法，用于添加约束组，属性`attributes`、`edges`、`size`、`center`

```objective-c
- (MASConstraint *)addConstraintWithAttributes:(MASAttribute)attrs {
    __unused MASAttribute anyAttribute = (MASAttributeLeft | MASAttributeRight 
                                          | MASAttributeTop | MASAttributeBottom 
                                          | MASAttributeLeading | MASAttributeTrailing 
                                          | MASAttributeWidth | MASAttributeHeight 
                                          | MASAttributeCenterX | MASAttributeCenterY 
                                          | MASAttributeBaseline | MASAttributeFirstBaseline 
                                          | MASAttributeLastBaseline | MASAttributeLeftMargin 
                                          | MASAttributeRightMargin | MASAttributeTopMargin 
                                          | MASAttributeBottomMargin | MASAttributeLeadingMargin 
                                          | MASAttributeTrailingMargin | MASAttributeCenterXWithinMargins
                                          | MASAttributeCenterYWithinMargins
                                          );
    
    NSAssert((attrs & anyAttribute) != 0, @"You didn't pass any attribute to make.attributes(...)");
    
    NSMutableArray *attributes = [NSMutableArray array];
    
    if (attrs & MASAttributeLeft) [attributes addObject:self.view.mas_left];
    if (attrs & MASAttributeRight) [attributes addObject:self.view.mas_right];
    if (attrs & MASAttributeTop) [attributes addObject:self.view.mas_top];
    if (attrs & MASAttributeBottom) [attributes addObject:self.view.mas_bottom];
    if (attrs & MASAttributeLeading) [attributes addObject:self.view.mas_leading];
    if (attrs & MASAttributeTrailing) [attributes addObject:self.view.mas_trailing];
    if (attrs & MASAttributeWidth) [attributes addObject:self.view.mas_width];
    if (attrs & MASAttributeHeight) [attributes addObject:self.view.mas_height];
    if (attrs & MASAttributeCenterX) [attributes addObject:self.view.mas_centerX];
    if (attrs & MASAttributeCenterY) [attributes addObject:self.view.mas_centerY];
    if (attrs & MASAttributeBaseline) [attributes addObject:self.view.mas_baseline];
    if (attrs & MASAttributeFirstBaseline) [attributes addObject:self.view.mas_firstBaseline];
    if (attrs & MASAttributeLastBaseline) [attributes addObject:self.view.mas_lastBaseline];
    if (attrs & MASAttributeLeftMargin) [attributes addObject:self.view.mas_leftMargin];
    if (attrs & MASAttributeRightMargin) [attributes addObject:self.view.mas_rightMargin];
    if (attrs & MASAttributeTopMargin) [attributes addObject:self.view.mas_topMargin];
    if (attrs & MASAttributeBottomMargin) [attributes addObject:self.view.mas_bottomMargin];
    if (attrs & MASAttributeLeadingMargin) [attributes addObject:self.view.mas_leadingMargin];
    if (attrs & MASAttributeTrailingMargin) [attributes addObject:self.view.mas_trailingMargin];
    if (attrs & MASAttributeCenterXWithinMargins) [attributes addObject:self.view.mas_centerXWithinMargins];
    if (attrs & MASAttributeCenterYWithinMargins) [attributes addObject:self.view.mas_centerYWithinMargins];
    
    NSMutableArray *children = [NSMutableArray arrayWithCapacity:attributes.count];
    
    for (MASViewAttribute *a in attributes) {
        [children addObject:[[MASViewConstraint alloc] initWithFirstViewAttribute:a]];
    }
    
    MASCompositeConstraint *constraint = [[MASCompositeConstraint alloc] initWithChildren:children];
    constraint.delegate = self;
    [self.constraints addObject:constraint];
    return constraint;
}
```

## install

`install`方法负责遍历创建所有`MSAConstraint`对象，并调用其`install`方法，将约束绑定在视图上

```objective-c
// 添加约束
- (NSArray *)install {
    // 调用remake时，会设置removeExisting = YES
    // 这时候需要先uninstall再install
    if (self.removeExisting) {
        // 获取所有约束
        NSArray *installedConstraints = 
            [MASViewConstraint installedConstraintsForView:self.view];
        // 移除所有约束
        for (MASConstraint *constraint in installedConstraints) {
            [constraint uninstall];
        }
    }
    // 添加约束，constraints存储的是调用时Block中的参数
    NSArray *constraints = self.constraints.copy;
    for (MASConstraint *constraint in constraints) {
        constraint.updateExisting = self.updateExisting;
        [constraint install];
    }
    [self.constraints removeAllObjects];
    return constraints;
}
```

[源码解读——Masonry](http://chuquan.me/2019/10/02/understand-masonry/)

[iOS开发之Masonry框架源码解析](https://www.cnblogs.com/ludashi/p/5591572.html)

[Masonry源码解读](https://www.jianshu.com/p/8990c5a98d29)
