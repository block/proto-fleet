import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Dices, Dice1, Dice2, Dice3, Dice4, Dice5, Dice6 } from 'lucide-react'
import { cn } from '@/lib/utils'

interface DiceButtonProps {
  onClick: (result: number) => void
  disabled?: boolean
}

const diceIcons = [Dice1, Dice2, Dice3, Dice4, Dice5, Dice6]

export function DiceButton({ onClick, disabled }: DiceButtonProps) {
  const [isRolling, setIsRolling] = useState(false)
  const [currentFace, setCurrentFace] = useState<number | null>(null)

  const handleClick = async () => {
    if (isRolling || disabled) return

    setIsRolling(true)
    setCurrentFace(null)

    // Simulate dice rolling animation
    let rollCount = 0
    const rollInterval = setInterval(() => {
      setCurrentFace(Math.floor(Math.random() * 6))
      rollCount++

      // Stop after 10 rolls (1 second)
      if (rollCount >= 10) {
        clearInterval(rollInterval)

        // Final result
        const finalResult = Math.floor(Math.random() * 6) + 1
        setCurrentFace(finalResult - 1) // Index is 0-based
        setIsRolling(false)

        // Trigger the callback after a short delay
        setTimeout(() => {
          onClick(finalResult)
        }, 300)
      }
    }, 100)
  }

  const DiceIcon = currentFace !== null ? diceIcons[currentFace] : Dices

  return (
    <Button
      onClick={handleClick}
      variant="outline"
      size="sm"
      disabled={disabled || isRolling}
      className={cn(
        "transition-all",
        isRolling && "animate-pulse"
      )}
      title={isRolling ? "Rolling..." : "Roll dice for random errors"}
    >
      <DiceIcon
        className={cn(
          "w-4 h-4 mr-2",
          isRolling && "animate-spin"
        )}
      />
      {isRolling ? 'Rolling...' : 'Random'}
    </Button>
  )
}